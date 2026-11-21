//go:build !android

package libSwipt

import (
	"net/netip"

	"github.com/sagernet/sing/common"
	"github.com/swipt-automation/swipt/internal/tailscale"
)

const (
	TailscaleStateStarting         = int32(tailscale.StateStarting)
	TailscaleStateStopped          = int32(tailscale.StateStopped)
	TailscaleStateNeedsLogin       = int32(tailscale.StateNeedsLogin)
	TailscaleStateNeedsMachineAuth = int32(tailscale.StateNeedsMachineAuth)
	TailscaleStateRunning          = int32(tailscale.StateRunning)
)

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
	controller *tailscale.Controller
}

type TailscaleLogin struct {
	login *tailscale.Login
}

func (p *Platform) NewTailscale(options *TailscaleOptions, handler TailscaleHandler) *Tailscale {
	return &Tailscale{controller: tailscale.NewController(tailscale.Options{
		Directory: options.Directory,
		Hostname:  options.Hostname,
		Version:   p.appVersion,
		Logger:    p.logFactory.NewLogger("tailscale"),
		OnStatus: func(status tailscale.Status) {
			handler.OnStatus(&TailscaleStatus{
				State:     int32(status.State),
				LoginName: status.LoginName,
				Tailnet:   status.Tailnet,
				DNSName:   status.DNSName,
				Addresses: newIterator(common.Map(status.Addresses, netip.Addr.String)),
			})
		},
		OnError: func(err error) { handler.OnError(err.Error()) },
	})}
}

func (t *Tailscale) SetEnabled(enabled bool) error {
	return t.controller.SetEnabled(enabled)
}

func (t *Tailscale) Login() (*TailscaleLogin, error) {
	login, err := t.controller.Login()
	if err != nil {
		return nil, err
	}
	return &TailscaleLogin{login: login}, nil
}

func (t *Tailscale) Logout() error {
	return t.controller.Logout()
}

func (l *TailscaleLogin) LoginURL() string {
	return l.login.URL()
}

func (l *TailscaleLogin) Complete() error {
	return l.login.Wait()
}

func (l *TailscaleLogin) Cancel() {
	l.login.Cancel()
}
