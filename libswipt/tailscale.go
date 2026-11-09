package libSwipt

import (
	"context"
	"net/netip"
	"sync"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/tailscale"
)

const (
	TailscaleStateStarting         = int32(tailscale.StateStarting)
	TailscaleStateStopped          = int32(tailscale.StateStopped)
	TailscaleStateNeedsLogin       = int32(tailscale.StateNeedsLogin)
	TailscaleStateNeedsMachineAuth = int32(tailscale.StateNeedsMachineAuth)
	TailscaleStateRunning          = int32(tailscale.StateRunning)
)

const tailscaleLogoutTimeout = 30 * time.Second

type TailscaleOptions struct {
	Directory string
	Hostname  string
}

type TailscaleStatus struct {
	State     int32
	LoginName string
	Tailnet   string
	DNSName   string
	Addresses StringIterator
}

type TailscaleHandler interface {
	OnStatus(status *TailscaleStatus)
	OnError(message string)
}

type Tailscale struct {
	options    tailscale.Options
	handler    TailscaleHandler
	lifecycle  sync.Mutex
	enabled    bool
	users      int
	node       *tailscale.Node
	access     sync.Mutex
	generation uint64
}

type TailscaleLogin struct {
	login *tailscale.Login
}

func (p *Platform) NewTailscale(options *TailscaleOptions, handler TailscaleHandler) *Tailscale {
	t := &Tailscale{
		options: tailscale.Options{
			Directory: options.Directory,
			Hostname:  options.Hostname,
			Version:   p.appVersion,
			Logger:    p.logFactory.NewLogger("tailscale"),
		},
		handler: handler,
	}
	t.reportStored(0)
	return t
}

func (t *Tailscale) SetEnabled(enabled bool) error {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	t.enabled = enabled
	if enabled {
		return t.start()
	}
	t.stop()
	return nil
}

func (t *Tailscale) Login() (*TailscaleLogin, error) {
	node, err := t.acquire()
	if err != nil {
		return nil, err
	}
	login, err := node.Login()
	if err != nil {
		t.release()
		return nil, err
	}
	go func() {
		<-login.Done()
		t.release()
	}()
	return &TailscaleLogin{login: login}, nil
}

func (t *Tailscale) Logout() error {
	node, err := t.acquire()
	if err != nil {
		return err
	}
	defer t.release()
	ctx, cancel := context.WithTimeout(context.Background(), tailscaleLogoutTimeout)
	defer cancel()
	return node.Logout(ctx)
}

func (t *Tailscale) runningNode() *tailscale.Node {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	return t.node
}

func (t *Tailscale) acquire() (*tailscale.Node, error) {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	t.users++
	err := t.start()
	if err != nil {
		t.users--
		return nil, err
	}
	return t.node, nil
}

func (t *Tailscale) release() {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	t.users--
	t.stop()
}

func (t *Tailscale) start() error {
	if t.node != nil {
		return nil
	}
	generation := t.nextGeneration()
	options := t.options
	options.OnStatus = func(status tailscale.Status) {
		t.report(generation, status)
	}
	options.OnError = func(err error) {
		t.access.Lock()
		defer t.access.Unlock()
		if generation == t.generation {
			t.handler.OnError(err.Error())
		}
	}
	node, err := tailscale.New(options)
	if err != nil {
		t.reportStored(generation)
		return err
	}
	t.node = node
	return nil
}

func (t *Tailscale) stop() {
	if t.node == nil || t.enabled || t.users > 0 {
		return
	}
	generation := t.nextGeneration()
	err := t.node.Close()
	if err != nil {
		t.options.Logger.Error(E.Cause(err, "close"))
	}
	t.node = nil
	t.reportStored(generation)
}

func (t *Tailscale) nextGeneration() uint64 {
	t.access.Lock()
	defer t.access.Unlock()
	t.generation++
	return t.generation
}

func (t *Tailscale) reportStored(generation uint64) {
	status, err := tailscale.StoredStatus(t.options.Directory)
	if err != nil {
		t.options.Logger.Error(err)
	}
	t.report(generation, status)
}

func (t *Tailscale) report(generation uint64, status tailscale.Status) {
	t.access.Lock()
	defer t.access.Unlock()
	if generation != t.generation {
		return
	}
	t.handler.OnStatus(&TailscaleStatus{
		State:     int32(status.State),
		LoginName: status.LoginName,
		Tailnet:   status.Tailnet,
		DNSName:   status.DNSName,
		Addresses: newIterator(common.Map(status.Addresses, netip.Addr.String)),
	})
}

func (l *TailscaleLogin) LoginURL() string {
	return l.login.URL()
}

func (l *TailscaleLogin) Wait() error {
	return l.login.Wait()
}

func (l *TailscaleLogin) Cancel() {
	l.login.Cancel()
}
