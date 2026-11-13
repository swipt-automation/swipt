package mdns

import (
	"context"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/net/dns/dnsmessage"
)

const queryInterval = time.Second

type Service struct {
	Port      uint16
	Text      map[string]string
	Addresses []netip.Addr
}

type instance struct {
	name     dnsmessage.Name
	service  *dnsmessage.SRVResource
	text     map[string]string
	reported *Service
}

type question struct {
	name string
	kind dnsmessage.Type
}

type browser struct {
	transport *transport
	service   dnsmessage.Name
	instances map[string]*instance
	hosts     map[string][]netip.Addr
	asked     map[question]bool
}

func Browse(ctx context.Context, serviceType string, found func(Service) bool) error {
	service, err := dnsmessage.NewName(strings.ToLower(serviceType) + ".local.")
	if err != nil {
		return E.Cause(err, "mdns: ", serviceType)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	b := &browser{
		transport: newTransport(0),
		service:   service,
		instances: make(map[string]*instance),
		hosts:     make(map[string][]netip.Addr),
		asked:     make(map[question]bool),
	}
	defer b.transport.close()
	if err = b.transport.refresh(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(queryInterval)
	defer ticker.Stop()
	err = b.query(true)
	for err == nil {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_ = b.transport.refresh(ctx)
			err = b.query(true)
		case received := <-b.transport.packets:
			if b.receive(received, found) {
				return nil
			}
			err = b.query(false)
		}
	}
	return err
}

func nameKey(name dnsmessage.Name) string {
	return strings.ToLower(name.String())
}

func keyOf(it dnsmessage.Question) question {
	return question{name: nameKey(it.Name), kind: it.Type}
}

func (b *browser) questions() []dnsmessage.Question {
	ask := func(name dnsmessage.Name, kind dnsmessage.Type) dnsmessage.Question {
		return dnsmessage.Question{Name: name, Type: kind, Class: dnsmessage.ClassINET}
	}
	wanted := []dnsmessage.Question{ask(b.service, dnsmessage.TypePTR)}
	for _, it := range b.instances {
		if it.service == nil {
			wanted = append(wanted, ask(it.name, dnsmessage.TypeSRV))
		} else if len(b.hosts[nameKey(it.service.Target)]) == 0 {
			wanted = append(wanted, ask(it.service.Target, dnsmessage.TypeA), ask(it.service.Target, dnsmessage.TypeAAAA))
		}
		if it.text == nil {
			wanted = append(wanted, ask(it.name, dnsmessage.TypeTXT))
		}
	}
	return common.UniqBy(wanted, keyOf)
}

func (b *browser) query(repeat bool) error {
	due := common.Filter(b.questions(), func(it dnsmessage.Question) bool { return repeat || !b.asked[keyOf(it)] })
	if len(due) == 0 {
		return nil
	}
	for _, it := range due {
		b.asked[keyOf(it)] = true
	}
	var failures []error
	for _, it := range b.transport.links {
		if err := b.transport.multicast(it, &dnsmessage.Message{Questions: due}); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) < len(b.transport.links) {
		return nil
	}
	return E.Errors(failures...)
}

func (b *browser) receive(received packet, found func(Service) bool) bool {
	parsed, isMessage := parse(received.data)
	if !isMessage || !parsed.header.Response {
		return false
	}
	var zone string
	if it, isLink := b.transport.linkOf(received.source.Addr()); isLink {
		zone = it.iface.Name
	}
	for _, record := range parsed.records() {
		b.cache(record, zone)
	}
	for _, it := range b.instances {
		service, resolved := b.resolve(it)
		if !resolved || it.reported != nil && sameService(*it.reported, service) {
			continue
		}
		it.reported = &service
		if found(service) {
			return true
		}
	}
	return false
}

func (b *browser) cache(record dnsmessage.Resource, zone string) {
	alive := record.Header.TTL > 0
	switch body := record.Body.(type) {
	case *dnsmessage.PTRResource:
		if sameName(record.Header.Name, b.service) {
			b.update(body.PTR, alive, func(*instance) {})
		}
	case *dnsmessage.SRVResource:
		b.update(record.Header.Name, alive, func(it *instance) { it.service = body })
	case *dnsmessage.TXTResource:
		b.update(record.Header.Name, alive, func(it *instance) { it.text = decodeTXT(body.TXT) })
	case *dnsmessage.AResource:
		b.address(record.Header.Name, netip.AddrFrom4(body.A), alive)
	case *dnsmessage.AAAAResource:
		address := netip.AddrFrom16(body.AAAA)
		if address.IsLinkLocalUnicast() {
			if zone == "" {
				return
			}
			address = address.WithZone(zone)
		}
		b.address(record.Header.Name, address, alive)
	}
}

func (b *browser) update(name dnsmessage.Name, alive bool, apply func(*instance)) {
	instanceKey := nameKey(name)
	if !strings.HasSuffix(instanceKey, "."+nameKey(b.service)) {
		return
	}
	if !alive {
		delete(b.instances, instanceKey)
		return
	}
	it := b.instances[instanceKey]
	if it == nil {
		it = &instance{name: name}
		b.instances[instanceKey] = it
	}
	apply(it)
}

func (b *browser) address(name dnsmessage.Name, address netip.Addr, alive bool) {
	hostKey := nameKey(name)
	addresses := slices.DeleteFunc(b.hosts[hostKey], func(it netip.Addr) bool { return it == address })
	if alive {
		addresses = append(addresses, address)
	}
	b.hosts[hostKey] = addresses
}

func (b *browser) resolve(it *instance) (Service, bool) {
	if it.service == nil || it.text == nil {
		return Service{}, false
	}
	addresses := b.hosts[nameKey(it.service.Target)]
	if len(addresses) == 0 {
		return Service{}, false
	}
	return Service{Port: it.service.Port, Text: it.text, Addresses: slices.Clone(addresses)}, true
}

func sameService(a Service, b Service) bool {
	return a.Port == b.Port && maps.Equal(a.Text, b.Text) && slices.Equal(a.Addresses, b.Addresses)
}

func decodeTXT(entries []string) map[string]string {
	text := make(map[string]string)
	for _, entry := range entries {
		name, value, _ := strings.Cut(entry, "=")
		if _, exists := text[name]; name != "" && !exists {
			text[name] = value
		}
	}
	return text
}
