package libSwipt

import (
	"context"
	"net/netip"
	"sync/atomic"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"
	"github.com/swipt-automation/swipt/internal/coredevice/vpnpeer"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/rfb"
)

const (
	DeviceStopped int32 = iota
	DeviceConnecting
	DevicePreparing
	DeviceConnected
	DeviceFailed
)

const (
	DeviceErrorNone             = int32(device.ProblemNone)
	DeviceErrorNoTunnel         = int32(device.ProblemNoTunnel)
	DeviceErrorPairingRejected  = int32(device.ProblemPairingRejected)
	DeviceErrorDeveloperModeOff = int32(device.ProblemDeveloperModeOff)
	DeviceErrorNoDiskImage      = int32(device.ProblemNoDiskImage)
	DeviceErrorDiskImage        = int32(device.ProblemDiskImage)
	DeviceErrorUnsupported      = int32(device.ProblemUnsupported)
	DeviceErrorScreenInUse      = int32(device.ProblemScreenInUse)
	DeviceErrorLoopbackAddress  = int32(device.ProblemLoopbackAddress)
)

type DeviceOptions struct {
	RemotePairingPort         int32
	DiskImageDirectory        string
	DeferAccessibilityRestart bool
}

type DeviceSettings struct {
	AutoUnlockPasscode     string
	AutoUnlockPasscodeType int32
}

type DeviceHandler interface {
	OnStateChanged(state int32, errorCode int32, message string)
	BacklightState() int32
}

const defaultRemotePairingPort = 49152

type LoopbackAddress struct {
	LocalNetwork bool
	address      netip.AddrPort
}

func newLoopbackAddress(peer vpnpeer.Peer, port uint16) *LoopbackAddress {
	return &LoopbackAddress{LocalNetwork: peer.LocalNetwork, address: netip.AddrPortFrom(peer.Address, port)}
}

type LoopbackProbe struct {
	ctx    context.Context
	cancel context.CancelFunc
	port   uint16
}

func NewLoopbackProbe(remotePairingPort int32) *LoopbackProbe {
	port := uint16(remotePairingPort)
	if port == 0 {
		port = defaultRemotePairingPort
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &LoopbackProbe{ctx: ctx, cancel: cancel, port: port}
}

func (p *LoopbackProbe) Run() (*LoopbackAddress, error) {
	defer p.cancel()
	peer, err := vpnpeer.Find(p.ctx, p.port, true)
	if p.ctx.Err() != nil {
		return nil, p.ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return newLoopbackAddress(peer, p.port), nil
}

func (p *LoopbackProbe) Cancel() {
	p.cancel()
}

type Device struct {
	logFactory  *log.Factory
	connection  *device.Connection
	services    *automation.Host
	pairingPort atomic.Uint32
	pairing     atomic.Pointer[pairingFile]
	loopback    atomic.Pointer[LoopbackAddress]
	unavailable atomic.Bool
}

func newDevice(options *DeviceOptions, handler DeviceHandler, logFactory *log.Factory) *Device {
	d := &Device{logFactory: logFactory}
	d.setPairingPort(options.RemotePairingPort)
	connectLogger := logFactory.NewLogger("device")
	d.connection = device.New(device.Options{
		Name: "iPhone",
		Connect: func(ctx context.Context) (*coredevice.Device, error) {
			file := d.pairing.Load()
			if file == nil {
				return nil, E.Cause1(device.ErrUnavailable, E.New("the device is not paired"))
			}
			port := uint16(d.pairingPort.Load())
			var udid string
			if file.lockdown != nil {
				port = lockdown.Port
				udid = file.lockdown.UDID
			} else {
				udid = file.remote.DeviceUDID
			}
			allowLocal := coredevice.IsGuest(udid)
			var localError error
			for {
				peer, err := vpnpeer.Find(ctx, port, allowLocal)
				var connected *coredevice.Device
				if err == nil {
					connectLogger.Info("device address: ", peer.Address, ", local network: ", peer.LocalNetwork)
					if file.lockdown != nil {
						connected, err = coredevice.ConnectLockdown(ctx, coredevice.LockdownOptions{Address: peer.Address, Record: file.lockdown, Logger: connectLogger})
					} else {
						connected, err = coredevice.ConnectRemotePairing(ctx, coredevice.RemotePairingOptions{
							Address: netip.AddrPortFrom(peer.Address, port), Record: file.remote, Logger: connectLogger,
						})
					}
					if err == nil {
						d.loopback.Store(newLoopbackAddress(peer, port))
						return connected, nil
					}
				}
				if !allowLocal || ctx.Err() != nil {
					return nil, E.Errors(localError, err)
				}
				connectLogger.Debug("local connection failed: ", err)
				localError = err
				allowLocal = false
			}
		},
		Reachable: func(ctx context.Context) error {
			return vpnpeer.Check(ctx, d.loopback.Load().address)
		},
		Logger:                    logFactory.Logger(),
		Observer:                  &deviceObserver{handler: handler},
		DiskImageDirectory:        options.DiskImageDirectory,
		DeferAccessibilityRestart: options.DeferAccessibilityRestart,
		BacklightState:            handler.BacklightState,
	})
	return d
}

func (d *Device) SetPairingRecord(record []byte) error {
	if len(record) == 0 {
		d.pairing.Store(nil)
		d.connection.Retry()
		return nil
	}
	file, err := parsePairingFile(record)
	if err != nil {
		return err
	}
	d.pairing.Store(&file)
	if file.remote != nil && file.remote.DeviceName != "" {
		d.connection.SetName(file.remote.DeviceName)
	}
	d.connection.Retry()
	d.services.RetryServices()
	return nil
}

func (d *Device) SetUnavailable(message string) {
	if message != "" {
		d.unavailable.Store(true)
		d.connection.SetUnavailable(E.New(message))
		return
	}
	d.connection.SetUnavailable(nil)
	if d.unavailable.Swap(false) {
		d.services.RetryServices()
	}
}

func (d *Device) UpdateSettings(settings *DeviceSettings) error {
	return d.connection.UpdateSettings(device.Settings{
		AutoUnlockPasscode:     settings.AutoUnlockPasscode,
		AutoUnlockPasscodeType: device.PasscodeType(settings.AutoUnlockPasscodeType),
	})
}

func (d *Device) LoopbackAddress() *LoopbackAddress {
	return d.loopback.Load()
}

func (d *Device) CheckRoute() {
	d.connection.CheckRoute()
}

func (d *Device) SetRemotePairingPort(port int32) {
	if d.setPairingPort(port) {
		d.connection.Retry()
		d.services.RetryServices()
	}
}

func (d *Device) setPairingPort(port int32) bool {
	if port == 0 {
		port = defaultRemotePairingPort
	}
	return d.pairingPort.Swap(uint32(port)) != uint32(port)
}

type deviceObserver struct {
	handler DeviceHandler
}

func (o *deviceObserver) StateChanged(state device.State, err error) {
	code, message := deviceError(err)
	o.handler.OnStateChanged(stateCode(state), code, message)
}

func (o *deviceObserver) DeviceConnected(info coredevice.DeviceInfo) {}

func (o *deviceObserver) ViewerConnected(session *rfb.ServerSession) {}

func (o *deviceObserver) ViewerDisconnected(session *rfb.ServerSession, err error) {}

func (o *deviceObserver) StreamingChanged(active bool) {}

func stateCode(state device.State) int32 {
	switch state {
	case device.StateConnecting:
		return DeviceConnecting
	case device.StatePreparing:
		return DevicePreparing
	case device.StateRunning:
		return DeviceConnected
	case device.StateFailed:
		return DeviceFailed
	}
	return DeviceStopped
}

func deviceError(err error) (int32, string) {
	if err == nil {
		return DeviceErrorNone, ""
	}
	return int32(device.ProblemOf(err)), err.Error()
}
