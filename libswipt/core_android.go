package libSwipt

import (
	"net"
	"os"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/android"
	"github.com/swipt-automation/swipt/internal/android/core"
)

const (
	shellHome = "/data/local/tmp/swipt"
	rootHome  = "/data/adb/swipt"
)

const (
	UnlockDisabled = int32(android.UnlockDisabled)
	UnlockNone     = int32(android.UnlockNone)
	UnlockPattern  = int32(android.UnlockPattern)
	UnlockPIN      = int32(android.UnlockPIN)
	UnlockPassword = int32(android.UnlockPassword)
)

type DeviceLayer interface {
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

type ScreenConsumer interface {
	Format(width int32, height int32, scale float64, encodedWidth int32, encodedHeight int32, encodedScale float64)
	Unit(data []byte, timeUs int64, turns int32)
	Drawn(nanoseconds int64, turns int32)
	WindowFailed(window int64, message string)
	Ended(message string)
}

type ScreenCapture interface {
	RequestKeyFrame()
	SetBitrate(bitrate int32)
	AddWindow(window int64) error
	RemoveWindow(window int64)
	SetEncoding(encoding bool)
	Close()
}

type layer struct {
	DeviceLayer
}

func (l layer) WatchState(name string, observer android.Observer) (android.Watch, error) {
	return l.DeviceLayer.WatchState(name, observer)
}

func (l layer) WatchEvents(name string, observer android.Observer) (android.Watch, error) {
	return l.DeviceLayer.WatchEvents(name, observer)
}

func (l layer) Advertise(name string, serviceType string, port int32, hostname string, attributes string, observer android.Observer) (android.Watch, error) {
	return l.DeviceLayer.Advertise(name, serviceType, port, hostname, attributes, observer)
}

func (l layer) Screenshot(display int32, consumer android.ScreenshotConsumer) error {
	return l.DeviceLayer.Screenshot(display, consumer)
}

func (l layer) OpenScreen(display int32, bitrate int32, consumer android.ScreenConsumer) (android.ScreenCapture, error) {
	return l.DeviceLayer.OpenScreen(display, bitrate, consumer)
}

type CoreOptions struct {
	APK        string
	AppVersion string
	AppBuild   string
}

type Core struct {
	core *core.Core
}

func CallCanceled(call int64) bool {
	return android.CallCanceled(call)
}

func home() string {
	if os.Getuid() == 0 {
		return rootHome
	}
	return shellHome
}

func loadEnvironment() error {
	environ, err := os.ReadFile("/proc/self/environ")
	if err != nil {
		return E.Cause(err, "read the environment")
	}
	for variable := range strings.SplitSeq(string(environ), "\x00") {
		if key, value, found := strings.Cut(variable, "="); found && key != "" {
			_ = os.Setenv(key, value)
		}
	}
	return nil
}

func StartCore(options *CoreOptions, device DeviceLayer) (*Core, error) {
	if err := loadEnvironment(); err != nil {
		return nil, err
	}
	directory := home()
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, err
	}
	started, err := core.Start(core.Options{
		Directory: directory, APK: options.APK, Layer: layer{device}, Root: os.Getuid() == 0,
		AppVersion: options.AppVersion, AppBuild: options.AppBuild, Logs: newLogFactory(),
	})
	if err != nil {
		return nil, err
	}
	return &Core{core: started}, nil
}

func (c *Core) Serve(fd int32) error {
	file := os.NewFile(uintptr(fd), "connection")
	defer file.Close()
	conn, err := net.FileConn(file)
	if err != nil {
		return E.Cause(err, "open the connection")
	}
	go c.core.Serve(conn)
	return nil
}

func (c *Core) Close() error {
	return c.core.Close()
}
