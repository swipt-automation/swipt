package mdns

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strconv"

	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const maxPacketSize = 9000

var (
	group4 = netip.MustParseAddrPort("224.0.0.251:5353")
	group6 = netip.MustParseAddrPort("[ff02::fb]:5353")
)

type multicastConn interface {
	JoinGroup(*net.Interface, net.Addr) error
	SetMulticastInterface(*net.Interface) error
}

type family struct {
	group   netip.AddrPort
	conn    *net.UDPConn
	options multicastConn
	joined  map[int]bool
}

type link struct {
	iface    net.Interface
	prefixes []netip.Prefix
}

type packet struct {
	data   []byte
	source netip.AddrPort
}

type transport struct {
	port     uint16
	families []*family
	links    []link
	packets  chan packet
}

func newTransport(port uint16) *transport {
	return &transport{port: port, families: []*family{{group: group4}, {group: group6}}, packets: make(chan packet)}
}

func (t *transport) refresh(ctx context.Context) error {
	var failures []error
	for _, it := range t.families {
		if it.conn == nil {
			failures = append(failures, t.open(ctx, it))
		}
	}
	links, err := listLinks()
	if err == nil {
		t.links = common.Filter(links, func(l link) bool {
			return slices.ContainsFunc(t.families, func(it *family) bool { return it.conn != nil && l.has(it) })
		})
	}
	if t.port == group4.Port() {
		for _, it := range t.families {
			it.join(t.links)
		}
	}
	if err == nil && slices.ContainsFunc(t.families, func(it *family) bool { return it.conn != nil }) {
		return nil
	}
	return E.Errors(append(failures, err)...)
}

func (t *transport) open(ctx context.Context, f *family) error {
	network := "udp6"
	if f.group.Addr().Is4() {
		network = "udp4"
	}
	listener := net.ListenConfig{Control: control.ReuseAddr()}
	conn, err := listener.ListenPacket(ctx, network, ":"+strconv.Itoa(int(t.port)))
	if err != nil {
		return E.Cause(err, "mdns: listen on ", network)
	}
	if f.group.Addr().Is4() {
		options := ipv4.NewPacketConn(conn)
		err = options.SetMulticastTTL(255)
		f.options = options
	} else {
		options := ipv6.NewPacketConn(conn)
		err = options.SetMulticastHopLimit(255)
		f.options = options
	}
	if err != nil {
		conn.Close()
		return E.Cause(err, "mdns: set the multicast hop limit on ", network)
	}
	f.conn = conn.(*net.UDPConn)
	go t.receive(ctx, f.conn)
	return nil
}

func (f *family) join(links []link) {
	if f.conn == nil {
		return
	}
	joined := make(map[int]bool)
	for _, it := range links {
		if !it.has(f) {
			continue
		}
		if !f.joined[it.iface.Index] {
			_ = f.options.JoinGroup(&it.iface, net.UDPAddrFromAddrPort(f.group))
		}
		joined[it.iface.Index] = true
	}
	f.joined = joined
}

func listLinks() ([]link, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, E.Cause(err, "mdns: list the network interfaces")
	}
	const required = net.FlagUp | net.FlagRunning | net.FlagMulticast
	var links []link
	for _, it := range interfaces {
		if it.Flags&required != required || it.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := it.Addrs()
		if err != nil {
			continue
		}
		current := link{iface: it}
		for _, address := range addresses {
			prefix, isPrefix := address.(*net.IPNet)
			if !isPrefix {
				continue
			}
			ip, _ := netip.AddrFromSlice(prefix.IP)
			ones, _ := prefix.Mask.Size()
			current.prefixes = append(current.prefixes, netip.PrefixFrom(ip.Unmap(), ones))
		}
		if len(current.prefixes) > 0 {
			links = append(links, current)
		}
	}
	return links, nil
}

func (l link) has(f *family) bool {
	return slices.ContainsFunc(l.prefixes, func(prefix netip.Prefix) bool {
		return prefix.Addr().Is4() == f.group.Addr().Is4()
	})
}

func (t *transport) linkOf(source netip.Addr) (link, bool) {
	index := slices.IndexFunc(t.links, func(it link) bool {
		if source.Zone() != "" {
			return it.iface.Name == source.Zone()
		}
		return slices.ContainsFunc(it.prefixes, func(prefix netip.Prefix) bool { return prefix.Contains(source) })
	})
	if index < 0 {
		return link{}, false
	}
	return t.links[index], true
}

func (t *transport) isOwn(source netip.Addr) bool {
	source = source.WithZone("")
	return slices.ContainsFunc(t.links, func(it link) bool {
		return slices.ContainsFunc(it.prefixes, func(prefix netip.Prefix) bool { return prefix.Addr() == source })
	})
}

func (t *transport) multicast(l link, message *dnsmessage.Message) error {
	packed, err := message.Pack()
	if err != nil {
		return E.Cause(err, "mdns: pack a message")
	}
	var failures []error
	sent := false
	for _, it := range t.families {
		if it.conn == nil || !l.has(it) {
			continue
		}
		err = it.options.SetMulticastInterface(&l.iface)
		if err == nil {
			_, err = it.conn.WriteToUDPAddrPort(packed, it.group)
		}
		if err != nil {
			failures = append(failures, err)
		} else {
			sent = true
		}
	}
	if sent || len(failures) == 0 {
		return nil
	}
	return E.Cause(E.Errors(failures...), "mdns: send to ", l.iface.Name)
}

func (t *transport) unicast(destination netip.AddrPort, message *dnsmessage.Message) error {
	packed, err := message.Pack()
	if err != nil {
		return E.Cause(err, "mdns: pack a message")
	}
	index := slices.IndexFunc(t.families, func(it *family) bool {
		return it.conn != nil && it.group.Addr().Is4() == destination.Addr().Is4()
	})
	if index < 0 {
		return E.New("mdns: no socket can reach ", destination)
	}
	_, err = t.families[index].conn.WriteToUDPAddrPort(packed, destination)
	return err
}

func (t *transport) receive(ctx context.Context, conn *net.UDPConn) {
	buffer := make([]byte, maxPacketSize)
	for {
		n, source, err := conn.ReadFromUDPAddrPort(buffer)
		if err != nil {
			return
		}
		select {
		case t.packets <- packet{data: slices.Clone(buffer[:n]), source: netip.AddrPortFrom(source.Addr().Unmap(), source.Port())}:
		case <-ctx.Done():
			return
		}
	}
}

func (t *transport) close() {
	for _, it := range t.families {
		if it.conn != nil {
			it.conn.Close()
		}
	}
}
