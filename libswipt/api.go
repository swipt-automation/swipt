package libSwipt

import (
	"context"
	"time"

	"github.com/swipt-automation/swipt/internal/api"
	"github.com/swipt-automation/swipt/internal/api/discovery"
)

const (
	APIMCPPath     = api.MCPPath
	APIServiceType = discovery.ServiceType
)

const apiCloseGrace = time.Second

type APIOptions struct {
	LocalNetwork      bool
	Port              int32
	LocalNetworkToken string
	Cloudflare        *CloudflareTunnel
	CloudflarePath    string
	CloudflareToken   string
	Tailscale         *Tailscale
	TailscaleToken    string
}

func (p *Platform) SetAPI(options *APIOptions) error {
	p.apiAccess.Lock()
	defer p.apiAccess.Unlock()
	if options == nil || !options.LocalNetwork && options.Cloudflare == nil && options.Tailscale == nil {
		server := p.api
		p.api = nil
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), apiCloseGrace)
			_ = server.Shutdown(ctx)
			cancel()
		}
		return nil
	}
	if p.api == nil {
		p.api = api.New(api.Options{
			Device: api.NewDevice(p.device.connection), Swiplets: p.host, Dialog: p.dialog, Logs: p.logFactory, Logger: p.logFactory.NewLogger("api"),
			AppVersion: p.appVersion, AppBuild: p.appBuild, Shutdown: func(context.Context) { p.handler.OnShutdownRequested() },
			OnPublishFailed: func(_ string, err error) { p.handler.OnAPIFailed(err.Error()) },
		})
	}
	publication := &api.Publication{
		LocalNetwork: options.LocalNetwork, Port: int(options.Port), LocalNetworkToken: options.LocalNetworkToken,
		CloudflarePath: options.CloudflarePath, CloudflareToken: options.CloudflareToken, TailscaleToken: options.TailscaleToken,
	}
	if options.Cloudflare != nil {
		publication.Cloudflare = options.Cloudflare.tunnel
	}
	if options.Tailscale != nil {
		publication.Tailscale = options.Tailscale.controller.RunningNode()
	}
	return p.api.Publish(publication)
}
