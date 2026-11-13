//go:build cgo

package computer

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"unicode"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/jsc"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/rfb"
	"github.com/swipt-automation/swipt/internal/usbmuxd"
)

const connectionUSB = "USB"

var ErrDeviceNotListed = E.New("device not connected to this computer or found on the local network")

type pairedDevice struct {
	UDID string
	Name string
}

func preferUSB(devices []usbmuxd.Device) []usbmuxd.Device {
	var result []usbmuxd.Device
	for _, it := range devices {
		index := slices.IndexFunc(result, func(known usbmuxd.Device) bool { return known.UDID == it.UDID })
		switch {
		case index < 0:
			result = append(result, it)
		case it.Connection == connectionUSB:
			result[index] = it
		}
	}
	return result
}

func dialer(id uint64) lockdown.Dialer {
	return func(ctx context.Context, port uint16) (net.Conn, error) {
		return usbmuxd.Dial(ctx, id, port)
	}
}

func usbmuxdFailure(err error) error {
	if operationError, isOperation := errors.AsType[*net.OpError](err); !isOperation || operationError.Op != "dial" {
		return err
	}
	if records, _ := remotePairings(); len(records) > 0 {
		return nil
	}
	return err
}

func chooseDevice(ctx context.Context, given string) (pairedDevice, error) {
	listed, err := usbmuxd.ListDevices(ctx)
	var devices []pairedDevice
	failures := []error{usbmuxdFailure(err)}
	for _, found := range preferUSB(listed) {
		value, err := lockdown.GetValue(ctx, dialer(found.ID), "DeviceName")
		if err != nil {
			if ctx.Err() != nil {
				return pairedDevice{}, err
			}
			err = E.Cause(err, "read the name of ", found.UDID)
			if strings.EqualFold(found.UDID, given) {
				return pairedDevice{}, err
			}
			failures = append(failures, err)
			continue
		}
		name, _ := value.(string)
		devices = append(devices, pairedDevice{UDID: found.UDID, Name: name})
	}
	network, err := browseNetwork(ctx, listed, "")
	if ctx.Err() != nil {
		return pairedDevice{}, ctx.Err()
	}
	failures = append(failures, err)
	for _, it := range network {
		devices = append(devices, pairedDevice{UDID: it.udid, Name: it.record.DeviceName})
	}
	if given != "" {
		devices = common.Filter(devices, func(it pairedDevice) bool {
			return strings.EqualFold(it.UDID, given) || strings.EqualFold(it.Name, given) || label(given) != "" && strings.EqualFold(label(it.Name), label(given))
		})
	}
	switch len(devices) {
	case 1:
		return devices[0], nil
	case 0:
		if err = E.Errors(failures...); err != nil {
			return pairedDevice{}, err
		}
		if given != "" {
			return pairedDevice{}, E.Extend(ErrDeviceNotListed, given)
		}
		return pairedDevice{}, ErrDeviceNotListed
	}
	names := common.Map(devices, func(it pairedDevice) string { return it.Name + " (" + it.UDID + ")" })
	return pairedDevice{}, E.New("more than one device is connected to this computer: ", strings.Join(names, ", "), "; choose one with -d NAME|UDID")
}

func label(name string) string {
	var parts []string
	var part strings.Builder
	for _, character := range strings.NewReplacer("'", "", "’", "").Replace(name) {
		if character < unicode.MaxASCII && (unicode.IsLetter(character) || unicode.IsDigit(character)) {
			part.WriteRune(character)
			continue
		}
		if part.Len() > 0 {
			parts = append(parts, part.String())
			part.Reset()
		}
	}
	if part.Len() > 0 {
		parts = append(parts, part.String())
	}
	return strings.Join(parts, "-")
}

type peer struct {
	device  usbmuxd.Device
	record  *lockdown.PairRecord
	network *networkDevice
}

func findPeer(ctx context.Context, udid string) (peer, error) {
	devices, listErr := usbmuxd.ListDevices(ctx)
	found := common.Find(preferUSB(devices), func(it usbmuxd.Device) bool { return it.UDID == udid })
	if found.ID == 0 {
		network, err := browseNetwork(ctx, devices, udid)
		if len(network) > 0 {
			return peer{network: &network[0]}, nil
		}
		if ctx.Err() != nil {
			return peer{}, ctx.Err()
		}
		return peer{}, &device.Error{Problem: device.ProblemNoTunnel, Err: E.Errors(E.New("the device is neither connected to this computer nor found on the local network"), listErr, err)}
	}
	data, err := usbmuxd.ReadPairRecord(ctx, udid)
	if err != nil {
		return peer{}, E.Cause(err, "read the pair record")
	}
	record, err := lockdown.ParsePairRecord(data)
	if err != nil {
		return peer{}, err
	}
	record.UDID = udid
	return peer{device: found, record: record}, nil
}

func (p peer) connect(ctx context.Context, logger log.ContextLogger) (*coredevice.Device, error) {
	if p.network != nil {
		return p.network.connect(ctx, logger)
	}
	if err := rememberRemotePairing(ctx, p, logger); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		logger.Warn("pair for connections over the local network: ", err)
	}
	logger.Debug("connecting through ", p.device.Connection, " as usbmuxd device ", p.device.ID)
	return coredevice.ConnectLockdown(ctx, coredevice.LockdownOptions{Record: p.record, Dial: dialer(p.device.ID), Logger: logger})
}

func connect(udid string, logger log.ContextLogger) func(ctx context.Context) (*coredevice.Device, error) {
	return func(ctx context.Context) (*coredevice.Device, error) {
		found, err := findPeer(ctx, udid)
		if err != nil {
			return nil, err
		}
		return found.connect(ctx, logger)
	}
}

type Session struct {
	peer   peer
	logger log.ContextLogger
	tunnel *coredevice.Device
}

func OpenSession(ctx context.Context, udid string, logger log.ContextLogger) (*Session, error) {
	found, err := findPeer(ctx, udid)
	if err != nil {
		return nil, err
	}
	return &Session{peer: found, logger: logger}, nil
}

func (s *Session) device(ctx context.Context) (*coredevice.Device, error) {
	if s.tunnel == nil {
		connected, err := s.peer.connect(ctx, s.logger)
		if err != nil {
			return nil, err
		}
		s.tunnel = connected
	}
	return s.tunnel, nil
}

func (s *Session) service(ctx context.Context, name string) (net.Conn, error) {
	if s.peer.network == nil {
		return lockdown.DialService(ctx, s.peer.record, dialer(s.peer.device.ID), name, s.logger)
	}
	tunnel, err := s.device(ctx)
	if err != nil {
		return nil, err
	}
	return tunnel.DialLockdownService(ctx, name)
}

func (s *Session) Close() {
	if s.tunnel != nil {
		s.tunnel.Close()
	}
}

type observer struct{}

func (observer) StateChanged(device.State, error) {}

func (observer) EnvironmentChanged(environment device.Environment) {
	jsc.SetLanguages(environment.Languages)
	jsc.SetTimeZone(environment.TimeZone)
}

func (observer) ViewerConnected(*rfb.ServerSession) {}

func (observer) ViewerDisconnected(*rfb.ServerSession, error) {}

func (observer) StreamingChanged(bool) {}
