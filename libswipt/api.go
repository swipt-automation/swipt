package libSwipt

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/api"
	"github.com/swipt-automation/swipt/internal/api/discovery"
	"github.com/swipt-automation/swipt/internal/tailscale"
)

const (
	APIMCPPath     = api.MCPPath
	APIServiceType = discovery.ServiceType
)

const (
	apiCloseGrace = time.Second
	tailnetPort   = 80
)

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

type apiState struct {
	server            *api.Server
	local             net.Listener
	localPort         int32
	localCredential   *api.Credential
	cloudflare        *CloudflareTunnel
	tunnel            net.Listener
	tunnelPath        string
	tunnelCredential  *api.Credential
	tailnetNode       *tailscale.Node
	tailnet           net.Listener
	tailnetCredential *api.Credential
}

func (p *Platform) serveAPI(state *apiState, listener net.Listener, prefix string, credential *api.Credential, name string) {
	if err := state.server.Serve(listener, prefix, credential); err != nil {
		err = E.Cause(err, "api: serve on ", name)
		p.logFactory.NewLogger("api").Error(err)
		p.handler.OnAPIFailed(err.Error())
	}
}

func (p *Platform) SetAPI(options *APIOptions) error {
	p.apiAccess.Lock()
	defer p.apiAccess.Unlock()
	state := p.api
	if options == nil || !options.LocalNetwork && options.Cloudflare == nil && options.Tailscale == nil {
		p.api = nil
		if state != nil {
			ctx, cancel := context.WithTimeout(context.Background(), apiCloseGrace)
			_ = state.server.Shutdown(ctx)
			cancel()
			closeListener(state.local)
			closeListener(state.tunnel)
			closeListener(state.tailnet)
		}
		return nil
	}
	if options.Cloudflare != nil && options.CloudflareToken == "" {
		return E.New("cloudflare: missing token")
	}
	tunnelPath := strings.TrimSuffix(options.CloudflarePath, "/")
	if options.Cloudflare != nil && tunnelPath != "" && !strings.HasPrefix(tunnelPath, "/") {
		return E.New("cloudflare: invalid path ", options.CloudflarePath)
	}
	if state == nil {
		state = &apiState{server: api.New(api.Options{
			Device: p.device.connection, Swiplets: p.host, Dialog: p.dialog, Logs: p.logFactory, Logger: p.logFactory.NewLogger("api"),
			AppVersion: p.appVersion, AppBuild: p.appBuild, Shutdown: func(context.Context) { p.handler.OnShutdownRequested() },
		})}
		state.tunnelCredential = state.server.NewCredential(options.CloudflareToken)
		p.api = state
	}
	state.tunnelCredential.SetToken(options.CloudflareToken)
	if state.cloudflare != options.Cloudflare || state.tunnelPath != tunnelPath {
		closeListener(state.tunnel)
		state.cloudflare, state.tunnel, state.tunnelPath = options.Cloudflare, nil, tunnelPath
		if options.Cloudflare != nil {
			listener, err := options.Cloudflare.tunnel.Listen()
			if err != nil {
				return err
			}
			state.tunnel = listener
			go p.serveAPI(state, listener, tunnelPath, state.tunnelCredential, "Cloudflare")
		}
	}
	var tailnetNode *tailscale.Node
	if options.Tailscale != nil {
		tailnetNode = options.Tailscale.runningNode()
	}
	authenticatesTailnet := options.TailscaleToken != ""
	if state.tailnetNode != tailnetNode || (state.tailnetCredential != nil) != authenticatesTailnet {
		closeListener(state.tailnet)
		state.tailnetNode, state.tailnet, state.tailnetCredential = tailnetNode, nil, nil
		if tailnetNode != nil {
			listener, err := tailnetNode.Listen(tailnetPort)
			if err != nil {
				return err
			}
			state.tailnet = listener
			if authenticatesTailnet {
				state.tailnetCredential = state.server.NewCredential(options.TailscaleToken)
			}
			go p.serveAPI(state, listener, "", state.tailnetCredential, "Tailscale")
		}
	} else if state.tailnetCredential != nil {
		state.tailnetCredential.SetToken(options.TailscaleToken)
	}
	authenticates := options.LocalNetworkToken != ""
	if !options.LocalNetwork || state.localPort != options.Port || (state.localCredential != nil) != authenticates {
		closeListener(state.local)
		state.local, state.localPort, state.localCredential = nil, 0, nil
	}
	if options.LocalNetwork && state.local == nil {
		listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(int(options.Port))))
		if err != nil {
			return E.Cause(err, "api: listen on port ", options.Port)
		}
		state.local, state.localPort = listener, options.Port
		if authenticates {
			state.localCredential = state.server.NewCredential(options.LocalNetworkToken)
		}
		go p.serveAPI(state, listener, "", state.localCredential, "the local network")
	} else if state.localCredential != nil {
		state.localCredential.SetToken(options.LocalNetworkToken)
	}
	return nil
}

func closeListener(listener net.Listener) {
	if listener != nil {
		_ = listener.Close()
	}
}
