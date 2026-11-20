package tailscale

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	tailscaleroot "github.com/sagernet/tailscale"
	"github.com/sagernet/tailscale/ipn"
	"github.com/sagernet/tailscale/ipn/ipnauth"
	"github.com/sagernet/tailscale/ipn/store"
	"github.com/sagernet/tailscale/tsnet"
	"github.com/sagernet/tailscale/version"
)

type State int32

const (
	StateStarting State = iota
	StateStopped
	StateNeedsLogin
	StateNeedsMachineAuth
	StateRunning
)

var setVersion sync.Once

type Options struct {
	Directory string
	Hostname  string
	Version   string
	OnStatus  func(Status)
	OnError   func(err error)
	Logger    logger.ContextLogger
}

type Status struct {
	State     State
	LoginName string
	Tailnet   string
	DNSName   string
	Addresses []netip.Addr
}

func (s Status) equal(other Status) bool {
	return s.State == other.State && s.LoginName == other.LoginName && s.Tailnet == other.Tailnet && s.DNSName == other.DNSName &&
		slices.Equal(s.Addresses, other.Addresses)
}

type Node struct {
	server   *tsnet.Server
	onStatus func(Status)
	onError  func(err error)
	logger   logger.ContextLogger
	ctx      context.Context
	cancel   context.CancelFunc
}

func New(options Options) (*Node, error) {
	setVersion.Do(func() {
		version.SetVersion(strings.TrimSpace(tailscaleroot.VersionDotTxt) + "-0-(Swipt " + options.Version + ")")
	})
	server := &tsnet.Server{
		Dir:      options.Directory,
		Hostname: options.Hostname,
		Logf: func(format string, args ...any) {
			options.Logger.Trace(fmt.Sprintf(format, args...))
		},
		UserLogf: func(format string, args ...any) {
			options.Logger.Debug(fmt.Sprintf(format, args...))
		},
	}
	err := server.Start()
	if err != nil {
		return nil, E.Cause(err, "tailscale: start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	node := &Node{
		server:   server,
		onStatus: options.OnStatus,
		onError:  options.OnError,
		logger:   options.Logger,
		ctx:      ctx,
		cancel:   cancel,
	}
	go node.watch()
	return node, nil
}

func (n *Node) Listen(port uint16) (net.Listener, error) {
	listener, err := n.server.Listen("tcp", ":"+strconv.Itoa(int(port)))
	if err != nil {
		return nil, E.Cause(err, "tailscale: listen on port ", port)
	}
	return listener, nil
}

func (n *Node) Logout(ctx context.Context) error {
	err := n.server.ExportLocalBackend().Logout(ctx, ipnauth.Self)
	if err != nil {
		return E.Cause(err, "tailscale: log out")
	}
	return nil
}

func (n *Node) Close() error {
	n.cancel()
	return n.server.Close()
}

func (n *Node) watch() {
	localBackend := n.server.ExportLocalBackend()
	wake := make(chan struct{}, 1)
	var (
		access   sync.Mutex
		messages []string
	)
	go localBackend.WatchNotifications(n.ctx, ipn.NotifyInitialState|ipn.NotifyInProcessNoDisconnect, nil, func(notify *ipn.Notify) bool {
		changed := notify.State != nil || notify.SelfChange != nil
		if notify.ErrMessage != nil {
			access.Lock()
			messages = append(messages, *notify.ErrMessage)
			access.Unlock()
			changed = true
		}
		if changed {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
		return true
	})
	var reported *Status
	for {
		select {
		case <-n.ctx.Done():
			return
		case <-wake:
		}
		access.Lock()
		pending := messages
		messages = nil
		access.Unlock()
		for _, message := range pending {
			err := E.New("tailscale: ", message)
			n.logger.Error(err)
			n.onError(err)
		}
		status := n.status()
		if reported == nil || !status.equal(*reported) {
			reported = &status
			n.onStatus(status)
		}
	}
}

func (n *Node) status() Status {
	localBackend := n.server.ExportLocalBackend()
	var status Status
	switch localBackend.State() {
	case ipn.NeedsLogin:
		return Status{State: StateNeedsLogin}
	case ipn.NeedsMachineAuth:
		status.State = StateNeedsMachineAuth
	case ipn.Running:
		status.State = StateRunning
	default:
		return Status{State: StateStarting}
	}
	profile := localBackend.CurrentProfile()
	status.LoginName = profile.UserProfile().LoginName()
	status.Tailnet = profile.NetworkProfile().DisplayNameOrDefault()
	current := localBackend.StatusWithoutPeers()
	if current.Self != nil {
		status.DNSName = strings.TrimSuffix(current.Self.DNSName, ".")
	}
	status.Addresses = current.TailscaleIPs
	return status
}

func StoredStatus(directory string) (Status, error) {
	loggedOut := Status{State: StateNeedsLogin}
	stateStore, err := store.NewFileStore(func(string, ...any) {}, filepath.Join(directory, "tailscaled.state"))
	if err != nil {
		return loggedOut, E.Cause(err, "tailscale: open state")
	}
	key, err := stateStore.ReadState(ipn.CurrentProfileStateKey)
	if errors.Is(err, ipn.ErrStateNotExist) {
		return loggedOut, nil
	} else if err != nil {
		return loggedOut, E.Cause(err, "tailscale: read current profile")
	}
	content, err := stateStore.ReadState(ipn.KnownProfilesStateKey)
	if errors.Is(err, ipn.ErrStateNotExist) {
		return loggedOut, nil
	} else if err != nil {
		return loggedOut, E.Cause(err, "tailscale: read profiles")
	}
	var profiles map[ipn.ProfileID]ipn.LoginProfile
	err = json.Unmarshal(content, &profiles)
	if err != nil {
		return loggedOut, E.Cause(err, "tailscale: decode profiles")
	}
	profile := common.Find(slices.Collect(maps.Values(profiles)), func(it ipn.LoginProfile) bool {
		return it.Key == ipn.StateKey(key)
	})
	if profile.Key == "" {
		return loggedOut, nil
	}
	content, err = stateStore.ReadState(profile.Key)
	if errors.Is(err, ipn.ErrStateNotExist) {
		return loggedOut, nil
	} else if err != nil {
		return loggedOut, E.Cause(err, "tailscale: read preferences")
	}
	var prefs ipn.Prefs
	err = ipn.PrefsFromBytes(content, &prefs)
	if err != nil {
		return loggedOut, E.Cause(err, "tailscale: decode preferences")
	}
	if prefs.LoggedOut || prefs.Persist == nil || prefs.Persist.PrivateNodeKey.IsZero() {
		return loggedOut, nil
	}
	return Status{
		State:     StateStopped,
		LoginName: profile.UserProfile.LoginName,
		Tailnet:   profile.NetworkProfile.DisplayNameOrDefault(),
	}, nil
}
