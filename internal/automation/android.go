//go:build android && cgo

package automation

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/android"
	"github.com/swipt-automation/swipt/internal/atomicfile"
)

const hostOS = Android

type Device = *android.Device

var deviceFailures map[string][]error

var deviceOperations = map[string]operationFunc{
	"device.info":                  androidInfo,
	"device.state":                 androidState,
	"device.unlock":                androidUnlock,
	"device.lock":                  androidLock,
	"device.createVirtualDisplay":  androidCreateVirtualDisplay,
	"device.releaseVirtualDisplay": androidReleaseVirtualDisplay,
	"device.clipboard":             androidClipboard,
	"device.setClipboard":          androidSetClipboard,
	"applications.installed":       androidRaw(android.Layer.InstalledApplications),
	"applications.icon":            androidApplicationIcon,
	"applications.launch":          androidLaunch,
	"notifications.list":           androidRaw(android.Layer.Notifications),
	"notifications.open":           androidNotification(android.Layer.OpenNotification),
	"notifications.dismiss":        androidNotification(android.Layer.DismissNotification),
	"notifications.perform":        androidPerformNotificationAction,
	"input.touch":                  androidTouch,
	"input.key":                    androidKey,
	"accessibility.snapshot":       androidAccessibilitySnapshot,
	"accessibility.changed":        androidAccessibilityChanged,
	"accessibility.waitForIdle":    androidAccessibilityIdle,
	"accessibility.aim":            androidAim,
	"accessibility.setText":        androidSetText,
	"accessibility.perform":        androidPerform,
	"shell.exec":                   androidExec,
	"shell.spawn":                  androidSpawn,
	"shell.wait":                   androidWait,
	"shell.kill":                   androidKill,
	"shell.killExited":             androidKillExited,
}

var deviceSubscriptions = map[string]subscriptionFunc{
	"device.watchState":     watchAndroid,
	"device.watchClipboard": watchAndroidClipboard,
	"applications.events":   watchAndroidEvents("applications"),
	"notifications.events":  watchAndroidEvents("notifications"),
}

var retriedOperations []string

func init() {
	for name, run := range deviceOperations {
		if name != "shell.spawn" && name != "accessibility.snapshot" {
			run = androidCanceled(run)
		}
		if name != "accessibility.changed" && name != "shell.wait" {
			run = androidAwake(run)
		}
		deviceOperations[name] = run
	}
}

type connectionWatch struct{}

func (connectionWatch) interrupted(time.Time) bool {
	return false
}

func (connectionWatch) dropCause() error {
	return nil
}

func (*Host) restoreDeviceState() {}

func (*Host) releaseDeviceState(string) {}

func (*Host) availability() (<-chan struct{}, error) {
	return nil, nil
}

func (*Host) deviceLost(error, time.Time) bool {
	return false
}

func (*runner) awaitReconnection(_ context.Context, err error, _ time.Time) (bool, error) {
	return false, err
}

func (c *GraphicsCall) Screenshot(display int) (*android.Pixels, error) {
	defer c.call.android().KeepAwake()()
	pixels, err := c.call.android().Screenshot(int32(display))
	if ctxErr := c.ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return pixels, androidError(err)
}

func (h *Host) KeepAwake() (release func()) {
	return h.options.Device.KeepAwake()
}

func (p Platform) MinimumOS() string {
	if p.MinimumSDK == 0 {
		return ""
	}
	return strconv.FormatInt(p.MinimumSDK, 10)
}

func PlatformRequiring(minimumOS string) Platform {
	sdk, _ := strconv.ParseInt(minimumOS, 10, 64)
	return Platform{MinimumSDK: sdk}
}

func (e *OSOutdatedError) Error() string {
	return "the swiplet requires Android SDK " + e.Minimum + ", and this device runs Android SDK " + e.Version
}

func (h *Host) checkOSVersion(platform Platform) error {
	osVersion := h.options.Environment().OSVersion
	if platform.MinimumSDK == 0 || osVersion == "" {
		return nil
	}
	if sdk, _ := strconv.ParseInt(osVersion, 10, 64); sdk < platform.MinimumSDK {
		return &OSOutdatedError{Minimum: strconv.FormatInt(platform.MinimumSDK, 10), Version: osVersion}
	}
	return nil
}

func androidCanceled(run operationFunc) operationFunc {
	return func(ctx context.Context, call *operation) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		value, err := run(ctx, call)
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return value, err
	}
}

func androidAwake(run operationFunc) operationFunc {
	return func(ctx context.Context, call *operation) (any, error) {
		defer call.android().KeepAwake()()
		return run(ctx, call)
	}
}

func (o *operation) android() *android.Device {
	return o.engine.host.options.Device
}

func androidError(err error) error {
	if failure, isFailure := errors.AsType[*android.Error](android.Failure(err)); isFailure {
		return &Failure{Name: failure.Name, Message: failure.Message}
	}
	return err
}

func (o *operation) androidParams(params any) error {
	var encoded json.RawMessage
	if err := o.decode(1, &encoded, new(string)); err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, params); err != nil {
		return E.Cause1(ErrInvalid, err)
	}
	return nil
}

func rawResult(value string, err error) (any, error) {
	if err != nil {
		return nil, androidError(err)
	}
	return json.RawMessage(value), nil
}

func androidRaw(read func(android.Layer) (string, error)) operationFunc {
	return func(_ context.Context, call *operation) (any, error) {
		if err := call.androidParams(&struct{}{}); err != nil {
			return nil, err
		}
		return rawResult(read(call.android().Layer))
	}
}

func androidInfo(ctx context.Context, call *operation) (any, error) {
	if err := call.androidParams(&struct{}{}); err != nil {
		return nil, err
	}
	encoded, err := call.android().Layer.Info()
	if err != nil {
		return nil, androidError(err)
	}
	var info map[string]json.RawMessage
	if err = json.Unmarshal([]byte(encoded), &info); err != nil {
		return nil, E.Cause(err, "read the device info")
	}
	described, err := android.ReadIdentity(ctx)
	if err != nil {
		return nil, err
	}
	info["name"], err = json.Marshal(described.Name)
	return info, err
}

func androidState(_ context.Context, call *operation) (any, error) {
	var params struct {
		State string `json:"state"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return rawResult(call.android().State(params.State))
}

type androidObserver struct {
	publish func(any)
	ended   func(error)
}

func (o *androidObserver) Next(value string) {
	o.publish(json.RawMessage(value))
}

func (o *androidObserver) Failed(message string) {
	o.ended(androidError(E.New(message)))
}

func watchAndroid(ctx context.Context, call *operation, publish func(any), ended func(error)) error {
	var params struct {
		State string `json:"state"`
	}
	var encoded json.RawMessage
	if err := call.decode(0, &encoded); err != nil {
		return err
	}
	if err := json.Unmarshal(encoded, &params); err != nil {
		return E.Cause1(ErrInvalid, err)
	}
	watch, err := call.android().WatchState(params.State, &androidObserver{publish: publish, ended: ended})
	if err != nil {
		return androidError(err)
	}
	go func() {
		<-ctx.Done()
		watch.Stop()
	}()
	return nil
}

func watchAndroidEvents(name string) subscriptionFunc {
	return func(ctx context.Context, call *operation, publish func(any), ended func(error)) error {
		if err := call.decode(0); err != nil {
			return err
		}
		watch, err := call.android().WatchEvents(name, &androidObserver{publish: publish, ended: ended})
		if err != nil {
			return androidError(err)
		}
		go func() {
			<-ctx.Done()
			watch.Stop()
		}()
		return nil
	}
}

func watchAndroidClipboard(ctx context.Context, call *operation, publish func(any), ended func(error)) error {
	if err := call.decode(0); err != nil {
		return err
	}
	return androidError(call.android().WatchClipboard(ctx, func(value json.RawMessage) { publish(value) }, func(err error) { ended(androidError(err)) }))
}

func androidUnlock(ctx context.Context, call *operation) (any, error) {
	if err := call.androidParams(&struct{}{}); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().Unlock(ctx))
}

func androidLock(ctx context.Context, call *operation) (any, error) {
	if err := call.androidParams(&struct{}{}); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().Lock(ctx))
}

func androidCreateVirtualDisplay(_ context.Context, call *operation) (any, error) {
	var params struct {
		Width      int32  `json:"width"`
		Height     int32  `json:"height"`
		DensityDpi int32  `json:"densityDpi"`
		Name       string `json:"name"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return rawResult(call.android().CreateVirtualDisplay(params.Width, params.Height, params.DensityDpi, params.Name))
}

func androidReleaseVirtualDisplay(_ context.Context, call *operation) (any, error) {
	var params struct {
		Display int32 `json:"display"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().ReleaseVirtualDisplay(params.Display))
}

func androidClipboard(ctx context.Context, call *operation) (any, error) {
	if err := call.androidParams(&struct{}{}); err != nil {
		return nil, err
	}
	value, err := call.android().Clipboard(ctx)
	if err != nil {
		return nil, androidError(err)
	}
	return value, nil
}

func androidNotification(act func(android.Layer, string) error) operationFunc {
	return func(_ context.Context, call *operation) (any, error) {
		var params struct {
			Key string `json:"key"`
		}
		if err := call.androidParams(&params); err != nil {
			return nil, err
		}
		return nil, androidError(act(call.android().Layer, params.Key))
	}
}

func androidPerformNotificationAction(_ context.Context, call *operation) (any, error) {
	var params json.RawMessage
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().PerformNotificationAction(string(params)))
}

func androidApplicationIcon(_ context.Context, call *operation) (any, error) {
	var packageName string
	if err := call.decode(1, &packageName); err != nil {
		return nil, err
	}
	device := call.android()
	updated, err := device.ApplicationUpdated(packageName)
	if err != nil {
		return nil, androidError(err)
	}
	directory := filepath.Join(call.engine.caches, "AppIcons")
	path := filepath.Join(directory, packageName+"@"+strconv.FormatInt(updated, 10)+".png")
	if _, err = os.Stat(path); err == nil {
		return fileURL(path), nil
	}
	icon, err := device.ApplicationIcon(packageName)
	if err != nil {
		return nil, androidError(err)
	}
	if err = os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	if err = atomicfile.Write(path, icon, 0o600); err != nil {
		return nil, err
	}
	return fileURL(path), nil
}

func androidLaunch(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Package string `json:"package"`
		Data    string `json:"data"`
		Restart bool   `json:"restart"`
		Display int32  `json:"display"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().LaunchApplication(ctx, params.Package, params.Data, params.Restart, params.Display))
}

func androidSetClipboard(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Text *string `json:"text"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().SetClipboard(ctx, params.Text))
}

func androidTouch(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Fingers json.RawMessage `json:"fingers"`
		Display int32           `json:"display"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().Touch(ctx, params.Display, string(params.Fingers)))
}

func androidKey(_ context.Context, call *operation) (any, error) {
	var params struct {
		Keys    json.RawMessage `json:"keys"`
		Display int32           `json:"display"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().PressKeys(params.Display, string(params.Keys)))
}

func androidAccessibilityChanged(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Seen      int64    `json:"seen"`
		TimeoutMs *float64 `json:"timeoutMs"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	timeout, err := androidMilliseconds("timeoutMs", params.TimeoutMs)
	if err != nil {
		return nil, err
	}
	return nil, androidError(call.android().WaitAccessibilityChange(ctx, params.Seen, timeout))
}

func androidAccessibilityIdle(ctx context.Context, call *operation) (any, error) {
	var params struct {
		IdleMs    float64  `json:"idleMs"`
		TimeoutMs *float64 `json:"timeoutMs"`
		Display   *int32   `json:"display"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	idle, err := androidMilliseconds("idleMs", &params.IdleMs)
	if err != nil {
		return nil, err
	}
	timeout, err := androidMilliseconds("timeoutMs", params.TimeoutMs)
	if err != nil {
		return nil, err
	}
	display := int32(-1)
	if params.Display != nil {
		display = *params.Display
	}
	reached, err := call.android().WaitAccessibilityIdle(ctx, display, idle, timeout)
	return reached, androidError(err)
}

func androidMilliseconds(name string, value *float64) (int64, error) {
	switch {
	case value == nil || *value >= math.MaxInt64:
		return math.MaxInt64, nil
	case *value < 0:
		return 0, E.Cause1(ErrInvalid, E.New(name, " must not be negative"))
	default:
		return int64(math.Ceil(*value)), nil
	}
}

func androidAim(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Element json.RawMessage `json:"element"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return rawResult(call.android().AimElement(ctx, string(params.Element)))
}

func androidSetText(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Element json.RawMessage `json:"element"`
		Text    string          `json:"text"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return rawResult(call.android().SetElementText(ctx, string(params.Element), params.Text))
}

func androidPerform(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Element   json.RawMessage `json:"element"`
		Action    string          `json:"action"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	return nil, androidError(call.android().PerformElementAction(ctx, string(params.Element), params.Action, string(params.Arguments)))
}
