package mdns

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
)

const (
	queryInterval = time.Second
	maxPacketSize = 9000
)

var group = netip.MustParseAddrPort("224.0.0.251:5353")

type Service struct {
	Port      uint16
	Text      map[string]string
	Addresses []netip.Addr
}

type socket struct {
	conn          *net.UDPConn
	interfaceName string
}

type packet struct {
	data          []byte
	source        netip.AddrPort
	interfaceName string
}

type instance struct {
	host   string
	port   uint16
	text   map[string]string
	source netip.Addr
}

func Browse(ctx context.Context, serviceType string, found func(Service) bool) error {
	domain := strings.ToLower(serviceType) + ".local."
	name, err := dnsmessage.NewName(domain)
	if err != nil {
		return E.Cause(err, "mdns: ", serviceType)
	}
	query, err := (&dnsmessage.Message{Questions: []dnsmessage.Question{{Name: name, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}}}).Pack()
	if err != nil {
		return err
	}
	sockets, err := listen()
	if err != nil {
		return err
	}
	defer func() {
		for _, it := range sockets {
			it.conn.Close()
		}
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	packets := make(chan packet)
	for _, it := range sockets {
		go receive(ctx, it, packets)
	}
	instances := make(map[string]*instance)
	hosts := make(map[string][]netip.Addr)
	reported := make(map[string]bool)
	ticker := time.NewTicker(queryInterval)
	defer ticker.Stop()
	if err = send(sockets, query); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err = send(sockets, query); err != nil {
				return err
			}
		case received := <-packets:
			if !parse(received, "."+domain, instances, hosts) {
				continue
			}
			for key, it := range instances {
				if reported[key] || it.port == 0 || it.text == nil || it.host == "" {
					continue
				}
				addresses := hosts[it.host]
				if len(addresses) == 0 {
					addresses = []netip.Addr{it.source}
				}
				reported[key] = true
				if found(Service{Port: it.port, Text: it.text, Addresses: addresses}) {
					return nil
				}
			}
		}
	}
}

func listen() ([]socket, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, E.Cause(err, "mdns: list the network interfaces")
	}
	var sockets []socket
	var failures []error
	for _, it := range interfaces {
		if it.Flags&net.FlagUp == 0 || it.Flags&net.FlagMulticast == 0 || it.Flags&net.FlagLoopback != 0 {
			continue
		}
		addresses, err := it.Addrs()
		if err != nil {
			continue
		}
		index := slices.IndexFunc(addresses, func(address net.Addr) bool {
			prefix, isPrefix := address.(*net.IPNet)
			return isPrefix && prefix.IP.To4() != nil
		})
		if index < 0 {
			continue
		}
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: addresses[index].(*net.IPNet).IP})
		if err == nil {
			multicast := ipv4.NewPacketConn(conn)
			err = multicast.SetMulticastInterface(&it)
			if err == nil {
				err = multicast.SetMulticastTTL(255)
			}
			if err != nil {
				conn.Close()
			}
		}
		if err != nil {
			failures = append(failures, E.Cause(err, "mdns: ", it.Name))
			continue
		}
		sockets = append(sockets, socket{conn: conn, interfaceName: it.Name})
	}
	if len(sockets) == 0 {
		if len(failures) > 0 {
			return nil, E.Errors(failures...)
		}
		return nil, E.New("mdns: no network interface supports multicast")
	}
	return sockets, nil
}

func send(sockets []socket, query []byte) error {
	var failures []error
	for _, it := range sockets {
		_, err := it.conn.WriteToUDPAddrPort(query, group)
		if err != nil {
			failures = append(failures, E.Cause(err, "mdns: send to ", it.interfaceName))
		}
	}
	if len(failures) < len(sockets) {
		return nil
	}
	return E.Errors(failures...)
}

func receive(ctx context.Context, it socket, packets chan<- packet) {
	buffer := make([]byte, maxPacketSize)
	for {
		n, source, err := it.conn.ReadFromUDPAddrPort(buffer)
		if err != nil {
			return
		}
		select {
		case packets <- packet{data: slices.Clone(buffer[:n]), source: netip.AddrPortFrom(source.Addr().Unmap(), source.Port()), interfaceName: it.interfaceName}:
		case <-ctx.Done():
			return
		}
	}
}

func parse(received packet, suffix string, instances map[string]*instance, hosts map[string][]netip.Addr) bool {
	var parser dnsmessage.Parser
	header, err := parser.Start(received.data)
	if err != nil || !header.Response {
		return false
	}
	if parser.SkipAllQuestions() != nil {
		return false
	}
	var resources []dnsmessage.Resource
	for _, section := range []func() ([]dnsmessage.Resource, error){parser.AllAnswers, parser.AllAuthorities, parser.AllAdditionals} {
		parsed, err := section()
		resources = append(resources, parsed...)
		if err != nil {
			break
		}
	}
	instanceOf := func(name string) *instance {
		if !strings.HasSuffix(name, suffix) {
			return nil
		}
		it := instances[name]
		if it == nil {
			it = &instance{source: received.source.Addr()}
			instances[name] = it
		}
		return it
	}
	for _, resource := range resources {
		name := strings.ToLower(resource.Header.Name.String())
		switch body := resource.Body.(type) {
		case *dnsmessage.SRVResource:
			if it := instanceOf(name); it != nil {
				it.host = strings.ToLower(body.Target.String())
				it.port = body.Port
			}
		case *dnsmessage.TXTResource:
			if it := instanceOf(name); it != nil {
				it.text = make(map[string]string)
				for _, entry := range body.TXT {
					key, value, _ := strings.Cut(entry, "=")
					it.text[key] = value
				}
			}
		case *dnsmessage.AResource:
			hosts[name] = appendAddress(hosts[name], netip.AddrFrom4(body.A))
		case *dnsmessage.AAAAResource:
			address := netip.AddrFrom16(body.AAAA)
			if address.IsLinkLocalUnicast() {
				address = address.WithZone(received.interfaceName)
			}
			hosts[name] = appendAddress(hosts[name], address)
		}
	}
	return true
}

func appendAddress(addresses []netip.Addr, address netip.Addr) []netip.Addr {
	if slices.Contains(addresses, address) {
		return addresses
	}
	return append(addresses, address)
}
