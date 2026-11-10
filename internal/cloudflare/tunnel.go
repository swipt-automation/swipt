package cloudflare

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"time"

	cloudflared "github.com/sagernet/sing-cloudflared"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/google/uuid"
)

type Mode int32

const (
	ModeQuickTunnel Mode = iota
	ModeTunnel
)

const maxRetryDelay = time.Minute

type Options struct {
	Mode     Mode
	Account  Account
	Hostname string
	State    string
	OnState  func(state string, hostname string)
	OnError  func(err error)
	Logger   logger.ContextLogger
}

type tunnelState struct {
	ID         uuid.UUID `json:"id"`
	AccountTag string    `json:"account_tag"`
	Secret     []byte    `json:"secret"`
	Hostname   string    `json:"hostname,omitempty"`
}

type Tunnel struct {
	mode     Mode
	account  Account
	hostname string
	onState  func(state string, hostname string)
	onError  func(err error)
	logger   logger.ContextLogger
	ctx      context.Context
	cancel   context.CancelFunc
	fallback *pipeListener
	idle     *http.Server

	access   sync.Mutex
	state    *tunnelState
	routed   bool
	attached *pipeListener
}

func New(options Options) *Tunnel {
	t := &Tunnel{
		mode:     options.Mode,
		account:  options.Account,
		hostname: options.Hostname,
		onState:  options.OnState,
		onError:  options.OnError,
		logger:   options.Logger,
		fallback: newPipeListener(nil),
		idle: &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, http.StatusText(http.StatusServiceUnavailable), http.StatusServiceUnavailable)
			}),
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	if t.logger == nil {
		t.logger = logger.NOP()
	}
	var state tunnelState
	if json.Unmarshal([]byte(options.State), &state) == nil && state.ID != uuid.Nil {
		t.state = &state
	}
	t.ctx, t.cancel = context.WithCancel(context.Background())
	go t.idle.Serve(t.fallback)
	go t.run()
	return t
}

func (t *Tunnel) Listen() (net.Listener, error) {
	var listener *pipeListener
	listener = newPipeListener(func() {
		t.access.Lock()
		if t.attached == listener {
			t.attached = nil
		}
		t.access.Unlock()
	})
	t.access.Lock()
	t.attached = listener
	t.access.Unlock()
	return listener, nil
}

func (t *Tunnel) Close() error {
	t.cancel()
	return t.idle.Close()
}

type tunnelDialer Tunnel

func (d *tunnelDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	t := (*Tunnel)(d)
	t.access.Lock()
	listener := t.attached
	t.access.Unlock()
	if listener != nil {
		conn, err := listener.dial(ctx)
		if !errors.Is(err, net.ErrClosed) {
			return conn, err
		}
	}
	return t.fallback.dial(ctx)
}

func (d *tunnelDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	return nil, os.ErrInvalid
}

func (t *Tunnel) run() {
	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			select {
			case <-t.ctx.Done():
				return
			case <-time.After(min(time.Second<<min(attempt-1, 6), maxRetryDelay)):
			}
		}
		t.route(t.ctx)
		var credentials *cloudflared.Credentials
		t.access.Lock()
		if t.state != nil {
			credentials = &cloudflared.Credentials{AccountTag: t.state.AccountTag, TunnelSecret: t.state.Secret, TunnelID: t.state.ID}
		}
		t.access.Unlock()
		service, err := cloudflared.NewService(cloudflared.ServiceOptions{
			Logger:           t.logger,
			ConnectionDialer: (*tunnelDialer)(t),
			LocallyManaged: &cloudflared.LocallyManagedOptions{
				Credentials: credentials,
				Service:     "http://localhost",
				Renew:       t.renew,
			},
			GracePeriod:   time.Second,
			ClientVersion: userAgent,
		})
		if err != nil {
			t.fail(E.Cause(err, "cloudflare: create the tunnel"))
			return
		}
		err = service.Start()
		if err == nil {
			<-t.ctx.Done()
		}
		service.Close()
		if t.ctx.Err() != nil {
			return
		}
		t.fail(E.Cause(err, "cloudflare: start the tunnel"))
	}
}

func (t *Tunnel) fail(err error) {
	t.logger.Error(err)
	if t.onError != nil {
		t.onError(err)
	}
}

func (t *Tunnel) route(ctx context.Context) {
	if t.mode != ModeTunnel {
		return
	}
	t.access.Lock()
	state, routed := t.state, t.routed
	t.access.Unlock()
	if state == nil || routed {
		return
	}
	for attempt := 0; ; attempt++ {
		err := t.account.routeTunnel(ctx, state.ID, t.hostname)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return
		}
		if _, unanswered := errors.AsType[*url.Error](err); !unanswered {
			t.fail(err)
			return
		}
		t.logger.Warn(err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(min(time.Second<<min(attempt, 6), maxRetryDelay)):
		}
	}
	t.access.Lock()
	if t.state == state {
		t.routed = true
	}
	t.access.Unlock()
}

func (t *Tunnel) renew(ctx context.Context) (cloudflared.Credentials, error) {
	var state tunnelState
	hostname := t.hostname
	if t.mode == ModeQuickTunnel {
		tunnel, err := cloudflared.RequestQuickTunnel(ctx, nil, userAgent)
		if err != nil {
			return cloudflared.Credentials{}, err
		}
		state = tunnelState{ID: tunnel.ID, AccountTag: tunnel.AccountTag, Secret: tunnel.Secret, Hostname: tunnel.Hostname}
		hostname = tunnel.Hostname
	} else {
		var err error
		state, err = t.account.createTunnel(ctx)
		if err != nil {
			return cloudflared.Credentials{}, err
		}
	}
	t.access.Lock()
	t.state = &state
	t.routed = false
	t.access.Unlock()
	content, err := json.Marshal(state)
	if err != nil {
		return cloudflared.Credentials{}, err
	}
	if t.onState != nil {
		t.onState(string(content), hostname)
	}
	t.route(ctx)
	return cloudflared.Credentials{AccountTag: state.AccountTag, TunnelSecret: state.Secret, TunnelID: state.ID}, nil
}
