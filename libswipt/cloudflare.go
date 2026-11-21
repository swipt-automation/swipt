//go:build !android

package libSwipt

import (
	"context"
	"encoding/json"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/cloudflare"
)

const (
	CloudflareModeQuickTunnel = int32(cloudflare.ModeQuickTunnel)
	CloudflareModeTunnel      = int32(cloudflare.ModeTunnel)
)

const zoneLookupTimeout = 30 * time.Second

type CloudflareTunnelOptions struct {
	Mode     int32
	Account  string
	Hostname string
	Tunnel   string
}

type CloudflareTunnelHandler interface {
	OnState(tunnel string, hostname string)
	OnError(message string)
}

type CloudflareTunnel struct {
	tunnel *cloudflare.Tunnel
}

type CloudflareAccount struct {
	Credential  string
	ZoneName    string
	AccountName string
}

type CloudflareLogin struct {
	login *cloudflare.Login
}

func NewCloudflareLogin() *CloudflareLogin {
	return &CloudflareLogin{login: cloudflare.NewLogin()}
}

func (l *CloudflareLogin) LoginURL() string {
	return l.login.URL()
}

func (l *CloudflareLogin) Complete() (*CloudflareAccount, error) {
	account, err := l.login.Wait()
	if err != nil {
		return nil, err
	}
	credential, err := json.Marshal(account)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), zoneLookupTimeout)
	defer cancel()
	zone, err := account.Zone(ctx)
	if err != nil {
		return nil, err
	}
	return &CloudflareAccount{Credential: string(credential), ZoneName: zone.Name, AccountName: zone.Account.Name}, nil
}

func (l *CloudflareLogin) Cancel() {
	l.login.Cancel()
}

func (p *Platform) NewCloudflareTunnel(options *CloudflareTunnelOptions, handler CloudflareTunnelHandler) (*CloudflareTunnel, error) {
	var account cloudflare.Account
	if options.Mode == CloudflareModeTunnel {
		var err error
		account, err = cloudflare.ParseAccount(options.Account)
		if err != nil {
			return nil, err
		}
		if options.Hostname == "" {
			return nil, E.New("cloudflare: missing hostname")
		}
	}
	return &CloudflareTunnel{tunnel: cloudflare.New(cloudflare.Options{
		Mode:     cloudflare.Mode(options.Mode),
		Account:  account,
		Hostname: options.Hostname,
		State:    options.Tunnel,
		OnState:  handler.OnState,
		OnError:  func(err error) { handler.OnError(err.Error()) },
		Logger:   p.logFactory.NewLogger("cloudflare"),
	})}, nil
}

func (t *CloudflareTunnel) Close() error {
	return t.tunnel.Close()
}
