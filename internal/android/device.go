//go:build android

package android

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"

	E "github.com/sagernet/sing/common/exceptions"
)

type Layer interface {
	Info() (string, error)
	Languages() (string, error)
	State(name string) (string, error)
	WatchState(name string, observer Observer) (Watch, error)
	WatchEvents(name string, observer Observer) (Watch, error)
	Clipboard() (string, error)
	SetClipboard(hasText bool, text string) error
	Advertise(name string, serviceType string, port int32, hostname string, attributes string, observer Observer) (Watch, error)
	SetAwake(awake bool)
	Screenshot(display int32, consumer ScreenshotConsumer) error
	Unlock(call int64, unlockType int32, credential string) error
	Lock(call int64) error
	CreateVirtualDisplay(width int32, height int32, densityDpi int32, name string) (string, error)
	ReleaseVirtualDisplay(display int32) error
	InstalledApplications() (string, error)
	ApplicationIcon(packageName string) ([]byte, error)
	ApplicationUpdated(packageName string) (int64, error)
	LaunchApplication(call int64, packageName string, data string, restart bool, display int32) error
	Notifications() (string, error)
	OpenNotification(key string) error
	DismissNotification(key string) error
	PerformNotificationAction(request string) error
	Touch(call int64, display int32, fingers string) error
	PressKeys(display int32, keys string) error
	AccessibilitySnapshot(call int64, display int32) (string, error)
	WaitAccessibilityChange(call int64, seen int64, timeoutMs int64) error
	WaitAccessibilityIdle(call int64, display int32, idleMs int64, timeoutMs int64) (bool, error)
	AimElement(call int64, element string) (string, error)
	SetElementText(call int64, element string, text string) (string, error)
	PerformElementAction(call int64, element string, action string, arguments string) error
	Interrupt(call int64)
	OpenScreen(display int32, bitrate int32, consumer ScreenConsumer) (ScreenCapture, error)
}

type Observer interface {
	Next(value string)
	Failed(message string)
}

type Watch interface {
	Stop()
}

type ScreenshotConsumer interface {
	Pixels(data []byte, width int32, height int32, stride int32)
}

type Pixels struct {
	Data   []byte
	Width  int
	Height int
	Stride int
}

func (p *Pixels) Pixels(data []byte, width int32, height int32, stride int32) {
	*p = Pixels{Data: bytes.Clone(data), Width: int(width), Height: int(height), Stride: int(stride)}
}

const (
	MinimumPasscode = 4
	MaximumPasscode = 16
	MaximumPattern  = 9
)

type UnlockType int32

const (
	UnlockDisabled UnlockType = iota
	UnlockNone
	UnlockPattern
	UnlockPIN
	UnlockPassword
)

type unlockCredential struct {
	unlockType UnlockType
	credential string
}

type Device struct {
	Layer
	root          bool
	credential    atomic.Pointer[unlockCredential]
	screensAccess sync.Mutex
	screens       map[int32]*screens
	helper        shellHelper
	sdk           int
	awakeAccess   sync.Mutex
	awake         int
}

type Info struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	OS           string `json:"os"`
	SDK          int    `json:"sdk"`
	Build        string `json:"build"`
	MDNSHostname bool   `json:"mdnsHostname"`
}

func New(layer Layer, apk string, root bool) (*Device, error) {
	device := &Device{Layer: layer, root: root, screens: make(map[int32]*screens), helper: shellHelper{apk: apk}}
	info, err := device.Info()
	if err != nil {
		return nil, err
	}
	device.sdk = info.SDK
	return device, nil
}

func (d *Device) Close() {
	d.helper.close()
}

func (d *Device) Info() (Info, error) {
	encoded, err := d.Layer.Info()
	if err != nil {
		return Info{}, Failure(err)
	}
	var info Info
	if err = json.Unmarshal([]byte(encoded), &info); err != nil {
		return Info{}, E.Cause(err, "read the device info")
	}
	return info, nil
}

func (d *Device) Screenshot(display int32) (*Pixels, error) {
	pixels := new(Pixels)
	err := d.Layer.Screenshot(display, pixels)
	if err != nil {
		return nil, Failure(err)
	}
	return pixels, nil
}

func (d *Device) KeepAwake() (release func()) {
	d.awakeAccess.Lock()
	d.awake++
	if d.awake == 1 {
		d.Layer.SetAwake(true)
	}
	d.awakeAccess.Unlock()
	return func() {
		d.awakeAccess.Lock()
		d.awake--
		if d.awake == 0 {
			d.Layer.SetAwake(false)
		}
		d.awakeAccess.Unlock()
	}
}

func (d *Device) SDK() int {
	return d.sdk
}

func (d *Device) Languages() []string {
	languages, err := d.Layer.Languages()
	if err != nil || languages == "" {
		return nil
	}
	return strings.Split(languages, ",")
}

func ValidateCredential(unlockType UnlockType, credential string) error {
	switch unlockType {
	case UnlockDisabled:
		return nil
	case UnlockNone:
		if credential != "" {
			return E.New("None or Swipe takes no unlock credential")
		}
		return nil
	case UnlockPattern:
		if len(credential) < MinimumPasscode || len(credential) > MaximumPattern {
			return E.New("an Android pattern has ", MinimumPasscode, " to ", MaximumPattern, " dots")
		}
		var seen [MaximumPattern]bool
		for index := range len(credential) {
			dot := credential[index]
			if dot < '1' || dot > '9' {
				return E.New("an Android pattern can contain only dots 1 to 9")
			}
			if seen[dot-'1'] {
				return E.New("an Android pattern cannot visit the same dot twice")
			}
			seen[dot-'1'] = true
		}
		return nil
	case UnlockPIN:
		if len(credential) < MinimumPasscode || len(credential) > MaximumPasscode {
			return E.New("an Android PIN has ", MinimumPasscode, " to ", MaximumPasscode, " digits")
		}
		for index := range len(credential) {
			if credential[index] < '0' || credential[index] > '9' {
				return E.New("an Android PIN can contain only digits from 0 to 9")
			}
		}
		return nil
	case UnlockPassword:
		if len(credential) < MinimumPasscode || len(credential) > MaximumPasscode {
			return E.New("an Android password has ", MinimumPasscode, " to ", MaximumPasscode, " characters")
		}
		for index := range len(credential) {
			if credential[index] < 0x20 || credential[index] > 0x7e {
				return E.New("an Android password can contain only printable ASCII characters")
			}
		}
		return nil
	default:
		return E.New("unknown unlock type ", unlockType)
	}
}

func (d *Device) SetCredential(unlockType UnlockType, credential string) {
	d.credential.Store(&unlockCredential{unlockType: unlockType, credential: credential})
}

func (d *Device) Unlock(ctx context.Context) error {
	var current unlockCredential
	if stored := d.credential.Load(); stored != nil {
		current = *stored
	}
	return d.run(ctx, func(id int64) error { return d.Layer.Unlock(id, int32(current.unlockType), current.credential) })
}
