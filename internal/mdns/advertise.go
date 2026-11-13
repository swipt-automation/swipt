package mdns

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	hostTTL          = 120
	serviceTTL       = 4500
	legacyTTL        = 10
	cacheFlush       = 1 << 15
	probeCount       = 3
	probeInterval    = 250 * time.Millisecond
	announceCount    = 2
	announceInterval = time.Second
	refreshInterval  = 5 * time.Second
)

var group6 = netip.MustParseAddrPort("[ff02::fb]:5353")

type Advertisement struct {
	cancel  context.CancelFunc
	done    chan struct{}
	claimed chan struct{}
	host    string
	failure error
}

type multicastConn interface {
	JoinGroup(*net.Interface, net.Addr) error
	SetMulticastInterface(*net.Interface) error
}

type family struct {
	group     netip.AddrPort
	conn      *net.UDPConn
	multicast multicastConn
}

type link struct {
	iface    net.Interface
	prefixes []netip.Prefix
}

type responder struct {
	name        string
	host        string
	serviceType dnsmessage.Name
	port        uint16
	txt         []string
	instance    dnsmessage.Name
	hostName    dnsmessage.Name
	families    []*family
	links       []link
	packets     chan packet
}

func Advertise(ctx context.Context, name string, serviceType string, host string, port uint16, txt map[string]string) (*Advertisement, error) {
	if strings.Contains(name, ".") || strings.Contains(host, ".") {
		return nil, E.New("mdns: advertise ", name, " on ", host, ": a name contains a dot")
	}
	serviceName, err := dnsmessage.NewName(strings.ToLower(serviceType) + ".local.")
	if err != nil {
		return nil, E.Cause(err, "mdns: ", serviceType)
	}
	r := &responder{
		name:        name,
		host:        host,
		serviceType: serviceName,
		port:        port,
		txt:         encodeTXT(txt),
		families:    []*family{{group: group}, {group: group6}},
		packets:     make(chan packet),
	}
	if err = r.rename(1); err == nil {
		_, err = (&dnsmessage.Message{Answers: r.records(link{})}).Pack()
	}
	if err != nil {
		return nil, E.Cause(err, "mdns: advertise ", name, " on ", host)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a := &Advertisement{cancel: cancel, done: make(chan struct{}), claimed: make(chan struct{})}
	go r.run(runCtx, a)
	select {
	case <-a.claimed:
		return a, nil
	case <-a.done:
		return nil, E.Cause(a.failure, "mdns: advertise ", name, " on ", host)
	case <-ctx.Done():
		a.Close()
		return nil, ctx.Err()
	}
}

// An empty TXT record holds one empty string (RFC 6763, section 6.1).
func encodeTXT(txt map[string]string) []string {
	if len(txt) == 0 {
		return []string{""}
	}
	entries := make([]string, 0, len(txt))
	for key, value := range txt {
		entries = append(entries, key+"="+value)
	}
	slices.Sort(entries)
	return entries
}

func (a *Advertisement) Host() string {
	return a.host
}

func (a *Advertisement) Close() {
	a.cancel()
	<-a.done
}

func (r *responder) rename(attempt int) error {
	name, host := r.name, r.host
	if attempt > 1 {
		name += " (" + strconv.Itoa(attempt) + ")"
		host += "-" + strconv.Itoa(attempt)
	}
	instance, err := dnsmessage.NewName(name + "." + r.serviceType.String())
	if err != nil {
		return err
	}
	hostName, err := dnsmessage.NewName(host + ".local.")
	if err != nil {
		return err
	}
	r.instance, r.hostName = instance, hostName
	return nil
}

func (r *responder) run(ctx context.Context, a *Advertisement) {
	defer close(a.done)
	defer func() {
		for _, it := range r.families {
			if it.conn != nil {
				it.conn.Close()
			}
		}
	}()
	refresh := time.NewTicker(refreshInterval)
	defer refresh.Stop()
	next := time.NewTimer(0)
	defer next.Stop()
	r.refresh(ctx)
	attempt, probes, announcements := 1, 0, announceCount
	probed := false
	for {
		select {
		case <-ctx.Done():
			if probed {
				for _, it := range r.links {
					r.announce(it, true)
				}
			}
			return
		case <-refresh.C:
			if r.refresh(ctx) && probed {
				announcements = announceCount
				next.Reset(0)
			}
		case received := <-r.packets:
			if probed {
				r.answer(received)
			} else if r.conflicts(received) {
				attempt++
				if err := r.rename(attempt); err != nil {
					a.failure = E.Cause(err, "rename after a conflict")
					return
				}
				probes = 0
				next.Reset(0)
			}
		case <-next.C:
			if probes < probeCount {
				for _, it := range r.links {
					r.probe(it)
				}
				probes++
				next.Reset(probeInterval)
				continue
			}
			if !probed {
				probed = true
				a.host = strings.TrimSuffix(r.hostName.String(), ".")
				close(a.claimed)
			}
			for _, it := range r.links {
				r.announce(it, false)
			}
			announcements--
			if announcements > 0 {
				next.Reset(announceInterval)
			}
		}
	}
}

func (r *responder) refresh(ctx context.Context) bool {
	opened := false
	for _, it := range r.families {
		if it.conn != nil {
			continue
		}
		network := "udp6"
		if it.group.Addr().Is4() {
			network = "udp4"
		}
		listener := net.ListenConfig{Control: control.ReuseAddr()}
		conn, err := listener.ListenPacket(ctx, network, ":"+strconv.Itoa(int(it.group.Port())))
		if err != nil {
			continue
		}
		if it.group.Addr().Is4() {
			multicast := ipv4.NewPacketConn(conn)
			err = multicast.SetMulticastTTL(255)
			it.multicast = multicast
		} else {
			multicast := ipv6.NewPacketConn(conn)
			err = multicast.SetMulticastHopLimit(255)
			it.multicast = multicast
		}
		if err != nil {
			conn.Close()
			continue
		}
		it.conn = conn.(*net.UDPConn)
		go receive(ctx, socket{conn: it.conn}, r.packets)
		opened = true
	}
	interfaces, err := net.Interfaces()
	if err != nil {
		return opened
	}
	var links []link
	for _, it := range interfaces {
		if it.Flags&(net.FlagUp|net.FlagRunning|net.FlagMulticast) != net.FlagUp|net.FlagRunning|net.FlagMulticast || it.Flags&net.FlagLoopback != 0 {
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
		if len(current.prefixes) == 0 {
			continue
		}
		for _, target := range r.families {
			if target.conn != nil && current.has(target) {
				_ = target.multicast.JoinGroup(&current.iface, net.UDPAddrFromAddrPort(target.group))
			}
		}
		links = append(links, current)
	}
	changed := opened || !slices.EqualFunc(r.links, links, func(previous link, current link) bool {
		return previous.iface.Index == current.iface.Index && slices.Equal(previous.prefixes, current.prefixes)
	})
	r.links = links
	return changed
}

func (l link) has(family *family) bool {
	return slices.ContainsFunc(l.prefixes, func(prefix netip.Prefix) bool {
		return prefix.Addr().Is4() == family.group.Addr().Is4()
	})
}

func (r *responder) records(l link) []dnsmessage.Resource {
	unique := dnsmessage.ClassINET | cacheFlush
	records := []dnsmessage.Resource{
		{
			Header: dnsmessage.ResourceHeader{Name: r.serviceType, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET, TTL: serviceTTL},
			Body:   &dnsmessage.PTRResource{PTR: r.instance},
		},
		{
			Header: dnsmessage.ResourceHeader{Name: r.instance, Type: dnsmessage.TypeSRV, Class: unique, TTL: hostTTL},
			Body:   &dnsmessage.SRVResource{Port: r.port, Target: r.hostName},
		},
		{
			Header: dnsmessage.ResourceHeader{Name: r.instance, Type: dnsmessage.TypeTXT, Class: unique, TTL: serviceTTL},
			Body:   &dnsmessage.TXTResource{TXT: r.txt},
		},
	}
	for _, prefix := range l.prefixes {
		if prefix.Addr().Is4() {
			records = append(records, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: r.hostName, Type: dnsmessage.TypeA, Class: unique, TTL: hostTTL},
				Body:   &dnsmessage.AResource{A: prefix.Addr().As4()},
			})
		} else {
			records = append(records, dnsmessage.Resource{
				Header: dnsmessage.ResourceHeader{Name: r.hostName, Type: dnsmessage.TypeAAAA, Class: unique, TTL: hostTTL},
				Body:   &dnsmessage.AAAAResource{AAAA: prefix.Addr().As16()},
			})
		}
	}
	return records
}

func (r *responder) send(l link, families []*family, message *dnsmessage.Message) {
	packed, err := message.Pack()
	if err != nil {
		return
	}
	for _, it := range families {
		if it.conn == nil || !l.has(it) || it.multicast.SetMulticastInterface(&l.iface) != nil {
			continue
		}
		_, _ = it.conn.WriteToUDPAddrPort(packed, it.group)
	}
}

func (r *responder) probe(l link) {
	r.send(l, r.families, &dnsmessage.Message{
		Questions: []dnsmessage.Question{
			{Name: r.instance, Type: dnsmessage.TypeALL, Class: dnsmessage.ClassINET},
			{Name: r.hostName, Type: dnsmessage.TypeALL, Class: dnsmessage.ClassINET},
		},
		Authorities: slices.DeleteFunc(r.records(l), func(record dnsmessage.Resource) bool { return record.Header.Type == dnsmessage.TypePTR }),
	})
}

func (r *responder) announce(l link, goodbye bool) {
	records := r.records(l)
	if goodbye {
		for index := range records {
			records[index].Header.TTL = 0
		}
	}
	r.send(l, r.families, &dnsmessage.Message{Header: dnsmessage.Header{Response: true, Authoritative: true}, Answers: records})
}

func (r *responder) conflicts(received packet) bool {
	var parser dnsmessage.Parser
	header, err := parser.Start(received.data)
	if err != nil || !header.Response || parser.SkipAllQuestions() != nil {
		return false
	}
	var owned []string
	for _, it := range r.links {
		for _, record := range r.records(it) {
			owned = append(owned, record.Body.GoString())
		}
	}
	for _, section := range []func() ([]dnsmessage.Resource, error){parser.AllAnswers, parser.AllAuthorities, parser.AllAdditionals} {
		resources, err := section()
		if slices.ContainsFunc(resources, func(resource dnsmessage.Resource) bool {
			name := resource.Header.Name.String()
			return (strings.EqualFold(name, r.instance.String()) || strings.EqualFold(name, r.hostName.String())) && !slices.Contains(owned, resource.Body.GoString())
		}) {
			return true
		}
		if err != nil {
			return false
		}
	}
	return false
}

func (r *responder) answer(received packet) {
	var parser dnsmessage.Parser
	header, err := parser.Start(received.data)
	if err != nil || header.Response || header.OpCode != 0 {
		return
	}
	questions, err := parser.AllQuestions()
	if err != nil {
		return
	}
	source := received.source.Addr()
	index := slices.IndexFunc(r.links, func(l link) bool {
		if source.Zone() != "" {
			return l.iface.Name == source.Zone()
		}
		return slices.ContainsFunc(l.prefixes, func(prefix netip.Prefix) bool { return prefix.Contains(source) })
	})
	if index < 0 {
		return
	}
	records := r.records(r.links[index])
	answered := make([]bool, len(records))
	related := make(map[string]bool)
	var answers []dnsmessage.Resource
	for _, question := range questions {
		if question.Class&^cacheFlush != dnsmessage.ClassINET && question.Class&^cacheFlush != dnsmessage.ClassANY {
			continue
		}
		for recordIndex, record := range records {
			if answered[recordIndex] || !strings.EqualFold(record.Header.Name.String(), question.Name.String()) || (question.Type != dnsmessage.TypeALL && question.Type != record.Header.Type) {
				continue
			}
			answered[recordIndex] = true
			answers = append(answers, record)
			related[strings.ToLower(record.Header.Name.String())] = true
			switch body := record.Body.(type) {
			case *dnsmessage.PTRResource:
				related[strings.ToLower(body.PTR.String())] = true
			case *dnsmessage.SRVResource:
				related[strings.ToLower(body.Target.String())] = true
			}
		}
	}
	if len(answers) == 0 {
		return
	}
	var additionals []dnsmessage.Resource
	for recordIndex, record := range records {
		if !related[strings.ToLower(record.Header.Name.String())] {
			continue
		}
		if service, isService := record.Body.(*dnsmessage.SRVResource); isService {
			related[strings.ToLower(service.Target.String())] = true
		}
		if !answered[recordIndex] {
			additionals = append(additionals, record)
		}
	}
	response := &dnsmessage.Message{Header: dnsmessage.Header{Response: true, Authoritative: true}, Answers: answers, Additionals: additionals}
	receiver := r.families[0]
	if source.Is6() {
		receiver = r.families[1]
	}
	if received.source.Port() == group.Port() {
		r.send(r.links[index], []*family{receiver}, response)
		return
	}
	response.Header.ID = header.ID
	response.Questions = questions
	for _, section := range [][]dnsmessage.Resource{answers, additionals} {
		for index := range section {
			section[index].Header.TTL = min(section[index].Header.TTL, legacyTTL)
			section[index].Header.Class &^= cacheFlush
		}
	}
	packed, err := response.Pack()
	if err == nil {
		_, _ = receiver.conn.WriteToUDPAddrPort(packed, received.source)
	}
}
