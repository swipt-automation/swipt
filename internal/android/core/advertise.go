//go:build android && cgo

package core

import (
	"strings"
	"unicode/utf8"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/android/proto"
	"github.com/swipt-automation/swipt/internal/api/discovery"
)

const labelLimit = 63

type advertisement struct {
	port          int
	authenticates bool
	stop          func()
}

func (c *Core) defaultLocalHostname() string {
	if c.hostname == "" {
		return ""
	}
	return c.hostname + ".local"
}

func (s *settingsService) advertise(local localNetwork, published bool) {
	s.advertising.Lock()
	defer s.advertising.Unlock()
	if current := s.advertisement; current != nil && (!published || current.port != local.Port || current.authenticates != local.Authenticates) {
		current.stop()
		s.advertisement = nil
		s.updateStatus(func(status *proto.PublicationStatus) { status.LocalNetworkHostname = s.core.defaultLocalHostname() })
	}
	if !published || s.advertisement != nil {
		return
	}
	s.updateStatus(func(status *proto.PublicationStatus) { status.LocalNetworkHostname = s.core.defaultLocalHostname() })
	started, err := s.startAdvertisement(local.Port, local.Authenticates)
	if err != nil {
		s.advertisementFailed(err)
		return
	}
	s.advertisement = started
}

func (s *settingsService) advertisementFailed(err error) {
	err = E.Cause(err, "advertise on the local network")
	s.logger.Error(err)
	s.updateStatus(func(status *proto.PublicationStatus) {
		status.LocalNetworkHostname = ""
		status.LocalNetworkError = err.Error()
	})
}

func (s *settingsService) startAdvertisement(port int, authenticates bool) (*advertisement, error) {
	name := s.core.identity.Name
	for len(name) > labelLimit {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	var attributes []string
	if authenticates {
		attributes = []string{discovery.AuthenticatesKey + "=1"}
	}
	started := &advertisement{port: port, authenticates: authenticates}
	stop, err := s.core.device.Advertise(
		strings.TrimRight(name, " "),
		discovery.ServiceType,
		port,
		s.core.hostname,
		attributes,
		func(hostname string) {
			s.advertising.Lock()
			active := s.advertisement == started
			s.advertising.Unlock()
			if active {
				s.updateStatus(func(status *proto.PublicationStatus) { status.LocalNetworkHostname = hostname + ".local" })
			}
		},
		func(err error) {
			s.advertising.Lock()
			active := s.advertisement == started
			if active {
				s.advertisement = nil
			}
			s.advertising.Unlock()
			if active {
				s.advertisementFailed(err)
			}
		},
	)
	if err != nil {
		return nil, err
	}
	started.stop = stop
	return started, nil
}

func localHostname(label string) string {
	label = hostLabel(label, "")
	if len(label) > labelLimit {
		label = strings.TrimRight(label[:labelLimit], "-")
	}
	return label
}
