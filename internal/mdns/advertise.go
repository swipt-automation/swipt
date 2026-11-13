package mdns

import (
	"context"
	"math/rand/v2"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	hostTTL          = 120
	serviceTTL       = 4500
	legacyTTL        = 10
	probeCount       = 3
	probeInterval    = 250 * time.Millisecond
	probeDefer       = time.Second
	announceCount    = 2
	announceInterval = time.Second
	refreshInterval  = 5 * time.Second
)

type Advertisement struct {
	responder *responder
	cancel    context.CancelFunc
	done      chan struct{}
	claimed   chan struct{}
	failure   error
}

type linkState struct {
	link
	probes        int
	claimed       bool
	announcements int
	due           time.Time
}

type responder struct {
	transport   *transport
	name        string
	host        string
	serviceType dnsmessage.Name
	port        uint16
	txt         []string
	attempt     int
	instance    dnsmessage.Name
	hostName    dnsmessage.Name
	current     atomic.Pointer[string]
	links       []*linkState
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
		transport:   newTransport(group4.Port()),
		name:        name,
		host:        host,
		serviceType: serviceName,
		port:        port,
		txt:         encodeTXT(txt),
	}
	if err = r.rename(); err == nil {
		_, err = (&dnsmessage.Message{Answers: r.records(link{})}).Pack()
	}
	if err != nil {
		return nil, E.Cause(err, "mdns: advertise ", name, " on ", host)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	a := &Advertisement{responder: r, cancel: cancel, done: make(chan struct{}), claimed: make(chan struct{})}
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
	return *a.responder.current.Load()
}

func (a *Advertisement) Close() {
	a.cancel()
	<-a.done
}

func (r *responder) rename() error {
	r.attempt++
	name, host := r.name, r.host
	if r.attempt > 1 {
		name += " (" + strconv.Itoa(r.attempt) + ")"
		host += "-" + strconv.Itoa(r.attempt)
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
	current := host + ".local"
	r.current.Store(&current)
	return nil
}

func (r *responder) run(ctx context.Context, a *Advertisement) {
	defer close(a.done)
	defer r.transport.close()
	refresh := time.NewTicker(refreshInterval)
	defer refresh.Stop()
	next := time.NewTimer(0)
	defer next.Stop()
	r.refresh(ctx)
	claimed := false
	for {
		if !claimed && !slices.ContainsFunc(r.links, func(it *linkState) bool { return !it.claimed }) {
			claimed = true
			close(a.claimed)
		}
		scheduled := common.Filter(r.links, func(it *linkState) bool { return !it.due.IsZero() })
		if len(scheduled) > 0 {
			next.Reset(time.Until(common.MinBy(scheduled, func(it *linkState) int64 { return it.due.UnixNano() }).due))
		} else {
			next.Stop()
		}
		select {
		case <-ctx.Done():
			for _, it := range r.links {
				if it.claimed {
					r.announce(it.link, true)
				}
			}
			return
		case <-refresh.C:
			r.refresh(ctx)
		case received := <-r.transport.packets:
			if err := r.receive(received); err != nil {
				a.failure = err
				return
			}
		case <-next.C:
			r.act(time.Now())
		}
	}
}

func (s *linkState) reset(due time.Time) {
	s.probes, s.claimed, s.announcements, s.due = 0, false, announceCount, due
}

func (r *responder) reprobe(due time.Time) {
	for _, it := range r.links {
		it.reset(due)
	}
}

func (r *responder) refresh(ctx context.Context) {
	_ = r.transport.refresh(ctx)
	links := make([]*linkState, 0, len(r.transport.links))
	for _, current := range r.transport.links {
		state := common.Find(r.links, func(it *linkState) bool {
			return it.iface.Index == current.iface.Index && slices.Equal(it.prefixes, current.prefixes)
		})
		if state == nil {
			state = &linkState{}
			state.reset(time.Now().Add(rand.N(probeInterval)))
		}
		state.link = current
		links = append(links, state)
	}
	r.links = links
}

func (r *responder) act(now time.Time) {
	for _, it := range r.links {
		if it.due.IsZero() || it.due.After(now) {
			continue
		}
		if it.probes < probeCount {
			r.probe(it.link)
			it.probes++
			it.due = now.Add(probeInterval)
			continue
		}
		it.claimed = true
		r.announce(it.link, false)
		it.announcements--
		it.due = time.Time{}
		if it.announcements > 0 {
			it.due = now.Add(announceInterval)
		}
	}
}

func (r *responder) receive(received packet) error {
	parsed, isMessage := parse(received.data)
	if !isMessage || parsed.header.OpCode != 0 {
		return nil
	}
	source := received.source.Addr()
	own := r.transport.isOwn(source)
	if parsed.header.Response {
		if own || !r.conflicts(parsed) {
			return nil
		}
		if !slices.ContainsFunc(r.links, func(it *linkState) bool { return it.claimed }) {
			if err := r.rename(); err != nil {
				return E.Cause(err, "rename after a conflict")
			}
		}
		r.reprobe(time.Now())
		return nil
	}
	arrived, isLink := r.transport.linkOf(source)
	state := common.Find(r.links, func(it *linkState) bool { return it.iface.Index == arrived.iface.Index })
	if !isLink || state == nil {
		return nil
	}
	if state.claimed {
		r.answer(state.link, parsed, received.source)
	} else if !own && r.loses(state.link, parsed.authorities) {
		r.reprobe(time.Now().Add(probeDefer))
	}
	return nil
}

func (r *responder) unique(l link) []dnsmessage.Resource {
	unique := dnsmessage.ClassINET | cacheFlush
	records := []dnsmessage.Resource{
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

func (r *responder) records(l link) []dnsmessage.Resource {
	return append([]dnsmessage.Resource{{
		Header: dnsmessage.ResourceHeader{Name: r.serviceType, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET, TTL: serviceTTL},
		Body:   &dnsmessage.PTRResource{PTR: r.instance},
	}}, r.unique(l)...)
}

func (r *responder) probe(l link) {
	_ = r.transport.multicast(l, &dnsmessage.Message{
		Questions: []dnsmessage.Question{
			{Name: r.instance, Type: dnsmessage.TypeALL, Class: dnsmessage.ClassINET},
			{Name: r.hostName, Type: dnsmessage.TypeALL, Class: dnsmessage.ClassINET},
		},
		Authorities: r.unique(l),
	})
}

func (r *responder) announce(l link, goodbye bool) {
	records := r.records(l)
	if goodbye {
		for index := range records {
			records[index].Header.TTL = 0
		}
	}
	_ = r.transport.multicast(l, &dnsmessage.Message{Header: dnsmessage.Header{Response: true, Authoritative: true}, Answers: records})
}

func (r *responder) owns(name dnsmessage.Name) bool {
	return sameName(name, r.instance) || sameName(name, r.hostName)
}

func (r *responder) conflicts(parsed message) bool {
	owned := common.FlatMap(r.links, func(it *linkState) []dnsmessage.Resource { return r.unique(it.link) })
	return slices.ContainsFunc(parsed.records(), func(record dnsmessage.Resource) bool {
		return record.Header.TTL > 0 && r.owns(record.Header.Name) && !slices.ContainsFunc(owned, func(it dnsmessage.Resource) bool { return sameRecord(it, record) })
	})
}

func (r *responder) loses(l link, authorities []dnsmessage.Resource) bool {
	ours := r.unique(l)
	for _, name := range []dnsmessage.Name{r.instance, r.hostName} {
		named := func(it dnsmessage.Resource) bool { return sameName(it.Header.Name, name) }
		theirs := common.Filter(authorities, named)
		if len(theirs) == 0 {
			continue
		}
		if order := compareRecords(common.Filter(ours, named), theirs); order != 0 {
			return order < 0
		}
	}
	return false
}

func (r *responder) answer(l link, query message, source netip.AddrPort) {
	records := r.records(l)
	answered := make([]bool, len(records))
	related := make(map[string]bool)
	var answers []dnsmessage.Resource
	for _, question := range query.questions {
		if question.Class&^cacheFlush != dnsmessage.ClassINET && question.Class&^cacheFlush != dnsmessage.ClassANY {
			continue
		}
		for index, record := range records {
			if answered[index] || !sameName(record.Header.Name, question.Name) || (question.Type != dnsmessage.TypeALL && question.Type != record.Header.Type) {
				continue
			}
			answered[index] = true
			answers = append(answers, record)
			related[nameKey(record.Header.Name)] = true
			switch body := record.Body.(type) {
			case *dnsmessage.PTRResource:
				related[nameKey(body.PTR)] = true
			case *dnsmessage.SRVResource:
				related[nameKey(body.Target)] = true
			}
		}
	}
	if len(answers) == 0 {
		return
	}
	var additionals []dnsmessage.Resource
	for index, record := range records {
		if !related[nameKey(record.Header.Name)] {
			continue
		}
		if service, isService := record.Body.(*dnsmessage.SRVResource); isService {
			related[nameKey(service.Target)] = true
		}
		if !answered[index] {
			additionals = append(additionals, record)
		}
	}
	response := &dnsmessage.Message{Header: dnsmessage.Header{Response: true, Authoritative: true}, Answers: answers, Additionals: additionals}
	if source.Port() == group4.Port() {
		_ = r.transport.multicast(l, response)
		return
	}
	response.Header.ID = query.header.ID
	response.Questions = query.questions
	for _, section := range [][]dnsmessage.Resource{answers, additionals} {
		for index := range section {
			section[index].Header.TTL = min(section[index].Header.TTL, legacyTTL)
			section[index].Header.Class &^= cacheFlush
		}
	}
	_ = r.transport.unicast(source, response)
}
