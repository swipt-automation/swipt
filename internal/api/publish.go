package api

import (
	"net"
	"strconv"
	"strings"
	"sync"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/cloudflare"
	"github.com/swipt-automation/swipt/internal/tailscale"
)

const tailnetPort = 80

const (
	NetworkLocal      = "the local network"
	NetworkCloudflare = "Cloudflare"
	NetworkTailscale  = "Tailscale"
)

type PublishError struct {
	Local      error
	Cloudflare error
	Tailscale  error
}

func (e *PublishError) Error() string {
	return E.Errors(e.Cloudflare, e.Tailscale, e.Local).Error()
}

func (e *PublishError) Unwrap() []error {
	return []error{e.Cloudflare, e.Tailscale, e.Local}
}

type Publication struct {
	LocalNetwork      bool
	Port              int
	LocalNetworkToken string
	Cloudflare        *cloudflare.Tunnel
	CloudflarePath    string
	CloudflareToken   string
	Tailscale         *tailscale.Node
	TailscaleToken    string
}

func (p *Publication) Empty() bool {
	return p == nil || !p.LocalNetwork && p.Cloudflare == nil && p.Tailscale == nil
}

type published struct {
	access            sync.Mutex
	local             net.Listener
	localPort         int
	localCredential   *Credential
	cloudflare        *cloudflare.Tunnel
	tunnel            net.Listener
	tunnelPath        string
	tunnelCredential  *Credential
	tailnetNode       *tailscale.Node
	tailnet           net.Listener
	tailnetCredential *Credential
}

func (s *Server) servePublished(listener net.Listener, prefix string, credential *Credential, name string) {
	if err := s.Serve(listener, prefix, credential); err != nil {
		err = E.Cause(err, "api: serve on ", name)
		s.options.Logger.Error(err)
		if s.options.OnPublishFailed != nil {
			s.options.OnPublishFailed(name, err)
		}
	}
}

func (s *Server) Publish(publication *Publication) error {
	state := &s.published
	state.access.Lock()
	defer state.access.Unlock()
	if publication.Empty() {
		closeListener(state.local)
		closeListener(state.tunnel)
		closeListener(state.tailnet)
		state.local, state.localPort, state.localCredential = nil, 0, nil
		state.cloudflare, state.tunnel, state.tunnelPath = nil, nil, ""
		state.tailnetNode, state.tailnet, state.tailnetCredential = nil, nil, nil
		return nil
	}

	var cloudflareErr error
	tunnelPath := strings.TrimSuffix(publication.CloudflarePath, "/")
	if publication.Cloudflare != nil {
		switch {
		case publication.CloudflareToken == "":
			cloudflareErr = E.New("cloudflare: missing token")
		case tunnelPath != "" && !strings.HasPrefix(tunnelPath, "/"):
			cloudflareErr = E.New("cloudflare: invalid path ", publication.CloudflarePath)
		}
	}
	if publication.Cloudflare == nil || cloudflareErr != nil || state.cloudflare != publication.Cloudflare || state.tunnelPath != tunnelPath {
		closeListener(state.tunnel)
		state.cloudflare, state.tunnel, state.tunnelPath = nil, nil, ""
	}
	if publication.Cloudflare != nil && cloudflareErr == nil {
		if state.tunnelCredential == nil {
			state.tunnelCredential = s.NewCredential(publication.CloudflareToken)
		}
		state.tunnelCredential.SetToken(publication.CloudflareToken)
		if state.tunnel == nil {
			listener, err := publication.Cloudflare.Listen()
			if err != nil {
				cloudflareErr = err
			} else {
				state.cloudflare, state.tunnel, state.tunnelPath = publication.Cloudflare, listener, tunnelPath
				go s.servePublished(listener, tunnelPath, state.tunnelCredential, NetworkCloudflare)
			}
		}
	}

	var tailscaleErr error
	authenticatesTailnet := publication.TailscaleToken != ""
	if publication.Tailscale == nil || state.tailnetNode != publication.Tailscale || (state.tailnetCredential != nil) != authenticatesTailnet {
		closeListener(state.tailnet)
		state.tailnetNode, state.tailnet, state.tailnetCredential = nil, nil, nil
	}
	if publication.Tailscale != nil {
		if state.tailnet == nil {
			listener, err := publication.Tailscale.Listen(tailnetPort)
			if err != nil {
				tailscaleErr = err
			} else {
				state.tailnetNode, state.tailnet = publication.Tailscale, listener
				if authenticatesTailnet {
					state.tailnetCredential = s.NewCredential(publication.TailscaleToken)
				}
				go s.servePublished(listener, "", state.tailnetCredential, NetworkTailscale)
			}
		} else if state.tailnetCredential != nil {
			state.tailnetCredential.SetToken(publication.TailscaleToken)
		}
	}

	var localErr error
	authenticates := publication.LocalNetworkToken != ""
	if !publication.LocalNetwork || state.localPort != publication.Port || (state.localCredential != nil) != authenticates {
		closeListener(state.local)
		state.local, state.localPort, state.localCredential = nil, 0, nil
	}
	if publication.LocalNetwork {
		if state.local == nil {
			listener, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(publication.Port)))
			if err != nil {
				localErr = E.Cause(err, "api: listen on port ", publication.Port)
			} else {
				state.local, state.localPort = listener, publication.Port
				if authenticates {
					state.localCredential = s.NewCredential(publication.LocalNetworkToken)
				}
				go s.servePublished(listener, "", state.localCredential, NetworkLocal)
			}
		} else if state.localCredential != nil {
			state.localCredential.SetToken(publication.LocalNetworkToken)
		}
	}

	if cloudflareErr == nil && tailscaleErr == nil && localErr == nil {
		return nil
	}
	return &PublishError{Local: localErr, Cloudflare: cloudflareErr, Tailscale: tailscaleErr}
}

func closeListener(listener net.Listener) {
	if listener != nil {
		_ = listener.Close()
	}
}
