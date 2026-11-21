//go:build !android && cgo

package automation

import (
	"context"
	"sync"
	"time"

	"github.com/swipt-automation/swipt/internal/coredevice/accessibility"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/version"
)

const (
	hostOS      = IOS
	springBoard = "com.apple.springboard"
)

type Device struct {
	phone       *device.Phone
	lease       *foregroundLease
	location    *locationSimulation
	appVersions *sync.Map
}

func NewDevice(phone *device.Phone, dialogApp string) Device {
	return Device{
		phone: phone, lease: &foregroundLease{phone: phone, app: dialogApp},
		location: &locationSimulation{changed: make(chan struct{})}, appVersions: &sync.Map{},
	}
}

var deviceFailures = map[string][]error{
	"UnavailableError": {device.ErrLocked, device.ErrStalled, accessibility.ErrStalled},
	"NotFoundError":    {accessibility.ErrGone},
	"AbortError":       {ErrDismissed},
}

var deviceOperations = map[string]operationFunc{
	"dialog":       presentDialog,
	"device.info":  deviceInfo,
	"device.state": deviceState,
	"device.unlock": screenAction(func(ctx context.Context, phone *device.Phone, _ *operation) error {
		return phone.Unlock(ctx)
	}),
	"device.installedApplications": deviceApps,
	"device.applicationIcon":       deviceAppIcon,
	"device.launchApplication":     screenAction(deviceLaunch),
	"device.pasteboard":            deviceClipboard,
	"device.setPasteboard":         deviceSetClipboard,
	"device.setSimulatedLocation":  deviceSetLocation,
	"input.tap":                    screenAction(deviceTap),
	"input.swipe":                  screenAction(deviceGesture),
	"input.drag":                   screenAction(deviceGesture),
	"input.pinch":                  screenAction(devicePinch),
	"input.touch":                  screenAction(deviceTouch),
	"input.pressKey":               screenAction(devicePressKey),
	"input.button":                 screenAction(deviceButton),
	"accessibility.snapshot":       uiSnapshot,
	"accessibility.find":           uiFind,
	"accessibility.changed":        uiChanged,
	"accessibility.setText":        screenAction(uiSetText),
	"accessibility.perform":        screenAction(uiPerform),
	"accessibility.tap":            screenAction(uiTap),
	"process.list":                 processList,
	"process.kill":                 processKill,
}

var deviceSubscriptions = map[string]subscriptionFunc{
	"device.state":             watchDeviceState,
	"device.applicationEvents": watchApplicationEvents,
	"process.resourceUsage":    watchResourceUsage,
}

var retriedOperations = []string{
	"device.info", "device.state", "device.installedApplications", "device.applicationIcon", "device.pasteboard", "process.list",
	"accessibility.snapshot", "accessibility.find", "accessibility.changed",
}

type connectionWatch struct {
	phone *device.Phone
}

func (w connectionWatch) interrupted(since time.Time) bool {
	return w.phone != nil && w.phone.Interrupted(since)
}

func (w connectionWatch) dropCause() error {
	if w.phone == nil {
		return nil
	}
	return w.phone.DropCause()
}

func (o *operation) phone() *device.Phone {
	o.connection.phone = o.engine.host.options.Device.phone
	return o.connection.phone
}

func (o *operation) screen(ctx context.Context) (*device.Phone, error) {
	if err := o.engine.host.options.Device.lease.wait(ctx); err != nil {
		return nil, err
	}
	return o.phone(), nil
}

func screenAction(act func(ctx context.Context, phone *device.Phone, call *operation) error) operationFunc {
	return func(ctx context.Context, call *operation) (any, error) {
		phone, err := call.screen(ctx)
		if err != nil {
			return nil, err
		}
		return nil, act(ctx, phone, call)
	}
}

func (c *GraphicsCall) Screenshot(int) ([]byte, float64, error) {
	capture := func() ([]byte, float64, error) {
		phone, err := c.call.screen(c.ctx)
		if err != nil {
			return nil, 0, err
		}
		return phone.Capture(c.ctx)
	}
	attempted := time.Now()
	data, scale, err := capture()
	if c.call.dropped(c.ctx, err, attempted) {
		attempted = time.Now()
		data, scale, err = capture()
	}
	if c.call.dropped(c.ctx, err, attempted) {
		err = c.call.droppedError(err, "during the screenshot")
	}
	return data, scale, err
}

func (p Platform) MinimumOS() string {
	return p.MinimumVersion
}

func PlatformRequiring(minimumOS string) Platform {
	return Platform{MinimumVersion: minimumOS}
}

func (e *OSOutdatedError) Error() string {
	return "the swiplet requires iOS " + e.Minimum + ", and this device runs iOS " + e.Version
}

func (h *Host) checkOSVersion(platform Platform) error {
	osVersion := h.options.Environment().OSVersion
	minimum, required := version.Parse(platform.MinimumVersion)
	if !required || osVersion == "" {
		return nil
	}
	if current, _ := version.Parse(osVersion); !current.AtLeast(minimum) {
		return &OSOutdatedError{Minimum: platform.MinimumVersion, Version: osVersion}
	}
	return nil
}
