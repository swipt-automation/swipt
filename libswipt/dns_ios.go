//go:build ios

package libSwipt

import (
	"context"
	"net"
	"sync/atomic"

	"github.com/sagernet/sing-tun/dnsinfo"
	E "github.com/sagernet/sing/common/exceptions"
)

var dnsServerIndex atomic.Uint32

func init() {
	net.DefaultResolver.Dial = dialSystemDNS
}

func dialSystemDNS(ctx context.Context, network, _ string) (net.Conn, error) {
	configuration := dnsinfo.Copy()
	if configuration == nil {
		return nil, E.New("dns: read the system DNS configuration")
	}
	servers := configuration.Select(0).Servers
	if len(servers) == 0 {
		return nil, E.New("dns: the system has no DNS server")
	}
	var dialer net.Dialer
	return dialer.DialContext(ctx, network, servers[dnsServerIndex.Add(1)%uint32(len(servers))].String())
}
