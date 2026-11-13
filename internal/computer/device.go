//go:build cgo

package computer

import (
	"context"
	"errors"
	"net"
	"slices"

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

var ErrDeviceNotListed = E.New("the device is neither connected to this computer nor found on the local network")

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
	if !errors.Is(err, usbmuxd.ErrUnavailable) {
		return err
	}
	if records, _ := remotePairings(); len(records) > 0 {
		return nil
	}
	return err
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
		return peer{}, &device.Error{Problem: device.ProblemNoTunnel, Err: E.Errors(E.Extend(ErrDeviceNotListed, udid), listErr, err)}
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

func (p peer) name(ctx context.Context) (string, error) {
	if p.network != nil {
		return p.network.record.DeviceName, nil
	}
	value, err := lockdown.GetValue(ctx, dialer(p.device.ID), "DeviceName")
	if err != nil {
		return "", E.Cause(err, "read the name of ", p.device.UDID)
	}
	name, _ := value.(string)
	return name, nil
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
	peer    peer
	virtual *virtualMachine
	logger  log.ContextLogger
	tunnel  *coredevice.Device
}

func OpenSession(ctx context.Context, udid string, logger log.ContextLogger) (*Session, error) {
	machine, err := findVirtualMachine(ctx, udid)
	if err != nil {
		return nil, err
	}
	found, err := findPeer(ctx, udid)
	if err != nil {
		return nil, err
	}
	return &Session{peer: found, virtual: machine, logger: logger}, nil
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
