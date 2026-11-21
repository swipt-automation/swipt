//go:build !android && cgo

package automation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/accessibility"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/fault"
	"github.com/swipt-automation/swipt/internal/selector"
)

type point device.Point

func (p *point) UnmarshalJSON(data []byte) error {
	values, ok := numbers(data, 2)
	if !ok {
		return E.New("expected a point [x, y] of finite numbers")
	}
	*p = point{values[0], values[1]}
	return nil
}

func numbers(data []byte, count int) ([]float64, bool) {
	var values []*float64
	if json.Unmarshal(data, &values) != nil || len(values) != count || slices.Contains(values, nil) {
		return nil, false
	}
	return common.Map(values, func(value *float64) float64 { return *value }), true
}

type touchOptions struct {
	Count *int `json:"count"`
}

func (o touchOptions) values() (int, error) {
	if o.Count != nil && *o.Count < 1 {
		return 0, E.Cause1(ErrInvalid, E.New("count must be at least 1"))
	}
	return common.PtrValueOrDefault(o.Count), nil
}

func deviceInfo(ctx context.Context, call *operation) (any, error) {
	if err := call.decode(0); err != nil {
		return nil, err
	}
	phone := call.phone()
	info, err := phone.Info(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name": info.Name, "model": info.Model, "os": info.OSVersion, "build": info.Build, "language": info.Language,
		"screen": map[string]any{
			"width": info.Screen.Width, "height": info.Screen.Height,
			"scale": info.Screen.Scale, "orientation": info.Screen.Orientation,
		},
	}, nil
}

func readState(ctx context.Context, phone *device.Phone, name string) (any, error) {
	switch name {
	case "foreground":
		return phone.Frontmost(ctx)
	case "locked":
		return phone.Locked(ctx)
	case "display":
		return phone.Display(ctx)
	}
	return nil, E.Cause1(ErrInvalid, E.New("unknown state ", strconv.Quote(name)))
}

func deviceState(ctx context.Context, call *operation) (any, error) {
	var name string
	if err := call.decode(1, &name); err != nil {
		return nil, err
	}
	phone := call.phone()
	if name == "simulatedLocation" {
		location, _ := call.engine.host.options.Device.location.read()
		return location, nil
	}
	return readState(ctx, phone, name)
}

func deviceTap(ctx context.Context, phone *device.Phone, call *operation) error {
	var at point
	var options touchOptions
	if err := call.decode(1, &at, &strict{&options}); err != nil {
		return err
	}
	count, err := options.values()
	if err != nil {
		return err
	}
	return phone.TapPoint(ctx, device.Point(at), count)
}

func deviceGesture(ctx context.Context, phone *device.Phone, call *operation) error {
	var from, to point
	var options struct {
		DurationMs *float64 `json:"durationMs"`
		PressMs    *float64 `json:"pressMs"`
	}
	if err := call.decode(2, &from, &to, &strict{&options}); err != nil {
		return err
	}
	move, err := duration("durationMs", options.DurationMs)
	if err != nil {
		return err
	}
	var fingers [][]device.TouchSample
	if call.name == "input.swipe" {
		if options.PressMs != nil {
			return E.Cause1(ErrInvalid, E.New("pressMs applies to drag"))
		}
		fingers, err = device.SwipePath(device.Point(from), device.Point(to), move)
	} else {
		if options.PressMs != nil && *options.PressMs < 0 {
			return E.Cause1(ErrInvalid, E.New("pressMs must not be negative"))
		}
		fingers, err = device.DragPath(device.Point(from), device.Point(to), move, milliseconds(common.PtrValueOrDefault(options.PressMs)))
	}
	if err != nil {
		return E.Cause1(ErrInvalid, err)
	}
	return phone.Touch(ctx, fingers)
}

func devicePinch(ctx context.Context, phone *device.Phone, call *operation) error {
	var center point
	var scale float64
	var options struct {
		DurationMs *float64 `json:"durationMs"`
	}
	if err := call.decode(2, &center, &scale, &strict{&options}); err != nil {
		return err
	}
	pinch, err := duration("durationMs", options.DurationMs)
	if err != nil {
		return err
	}
	return phone.Pinch(ctx, device.Point(center), scale, pinch)
}

func deviceTouch(ctx context.Context, phone *device.Phone, call *operation) error {
	var fingers [][][3]*float64
	if err := call.decode(1, &fingers); err != nil {
		return err
	}
	samples := make([][]device.TouchSample, len(fingers))
	for index, finger := range fingers {
		for _, sample := range finger {
			if slices.Contains(sample[:], nil) {
				return E.Cause1(ErrInvalid, E.New("touch samples need finite numbers [x, y, ms]"))
			}
			samples[index] = append(samples[index], device.TouchSample{X: *sample[0], Y: *sample[1], T: milliseconds(*sample[2])})
		}
	}
	return phone.Touch(ctx, samples)
}

func devicePressKey(ctx context.Context, phone *device.Phone, call *operation) error {
	var combo string
	if err := call.decode(1, &combo); err != nil {
		return err
	}
	return phone.Key(ctx, combo)
}

func deviceButton(ctx context.Context, phone *device.Phone, call *operation) error {
	var name string
	if err := call.decode(1, &name); err != nil {
		return err
	}
	return phone.Button(ctx, name)
}

func deviceLaunch(ctx context.Context, phone *device.Phone, call *operation) error {
	var bundleID string
	var options struct {
		URL     string `json:"url"`
		Restart bool   `json:"restart"`
	}
	if err := call.decode(1, &bundleID, &strict{&options}); err != nil {
		return err
	}
	return phone.Launch(ctx, bundleID, options.URL, options.Restart, true)
}

func deviceClipboard(ctx context.Context, call *operation) (any, error) {
	if err := call.decode(0); err != nil {
		return nil, err
	}
	phone := call.phone()
	text, found, err := phone.Clipboard(ctx)
	if err != nil || !found {
		return nil, err
	}
	return text, nil
}

func deviceSetClipboard(ctx context.Context, call *operation) (any, error) {
	if len(call.args) == 0 {
		return nil, E.Cause1(ErrInvalid, E.New("argument 1 is missing"))
	}
	var text *string
	if err := call.decode(0, &text); err != nil {
		return nil, err
	}
	phone := call.phone()
	return nil, phone.SetClipboard(ctx, text)
}

func deviceSetLocation(ctx context.Context, call *operation) (any, error) {
	if len(call.args) == 0 {
		return nil, E.Cause1(ErrInvalid, E.New("argument 1 is missing"))
	}
	var location *struct {
		Latitude  *float64 `json:"latitude"`
		Longitude *float64 `json:"longitude"`
	}
	if err := call.decode(0, &location); err != nil {
		return nil, err
	}
	phone := call.phone()
	if location == nil {
		return nil, call.engine.host.clearLocation(ctx, phone)
	}
	if location.Latitude == nil || *location.Latitude < -90 || *location.Latitude > 90 || location.Longitude == nil || *location.Longitude < -180 || *location.Longitude > 180 {
		return nil, E.Cause1(ErrInvalid, E.New("the latitude must be within ±90 and the longitude within ±180 degrees"))
	}
	return nil, call.engine.host.setLocation(ctx, phone, call.engine.swiplet.ID, *location.Latitude, *location.Longitude)
}

type snapshotOptions struct {
	Scope  string `json:"scope"`
	Full   bool   `json:"full"`
	Depth  *int64 `json:"depth"`
	Within string `json:"within"`
}

func depthOf(depth *int64) int64 {
	if depth == nil {
		return accessibility.DefaultDepth
	}
	return *depth
}

var snapshotScopes = map[string]device.SnapshotScope{
	"": device.ScopeFrontApp, "frontApp": device.ScopeFrontApp, "springBoard": device.ScopeSpringBoard, "fullScreen": device.ScopeFullScreen,
}

func uiSnapshot(ctx context.Context, call *operation) (any, error) {
	var options snapshotOptions
	if err := call.decode(0, &strict{&options}); err != nil {
		return nil, err
	}
	scope, known := snapshotScopes[options.Scope]
	switch {
	case !known:
		return nil, E.Cause1(ErrInvalid, E.New("scope must be frontApp, springBoard or fullScreen"))
	case options.Scope != "" && options.Within != "":
		return nil, E.Cause1(ErrInvalid, E.New("scope and within are exclusive"))
	}
	phone, err := call.screen(ctx)
	if err != nil {
		return nil, err
	}
	view, err := phone.Snapshot(ctx, device.SnapshotOptions{Scope: scope, Raw: options.Full, Depth: depthOf(options.Depth), Within: options.Within})
	if err != nil {
		return nil, err
	}
	return snapshot{ID: call.engine.snapshots.keep(iosSnapshot{view}), View: view}, nil
}

type findOptions struct {
	Depth *int64 `json:"depth"`
}

func uiFind(ctx context.Context, call *operation) (any, error) {
	var encoded []json.RawMessage
	var options findOptions
	if err := call.decode(1, &encoded, &strict{&options}); err != nil {
		return nil, err
	}
	selectors := make([]*device.Selector, 0, len(encoded))
	for _, data := range encoded {
		parsed, err := selector.Parse(device.Vocabulary, data)
		if err != nil {
			return nil, err
		}
		selectors = append(selectors, parsed)
	}
	phone, err := call.screen(ctx)
	if err != nil {
		return nil, err
	}
	view, seen, err := phone.Find(ctx, selectors, depthOf(options.Depth))
	if errors.Is(err, fault.ErrNotAnswering) {
		return map[string]any{"snapshot": nil, "seen": seen, "failure": FailureOf(err)}, nil
	}
	if err != nil {
		return nil, err
	}
	var found any
	if view != nil {
		found = snapshot{ID: call.engine.snapshots.keep(iosSnapshot{view}), View: view}
	}
	return map[string]any{"snapshot": found, "seen": seen}, nil
}

func uiChanged(ctx context.Context, call *operation) (any, error) {
	var seen uint64
	var timeoutMs *float64
	if err := call.decode(1, &seen, &timeoutMs); err != nil {
		return nil, err
	}
	timeout := time.Duration(-1)
	if timeoutMs != nil {
		if *timeoutMs < 0 {
			return nil, E.Cause1(ErrInvalid, E.New("timeoutMs must not be negative"))
		}
		timeout = milliseconds(*timeoutMs)
	}
	phone, err := call.screen(ctx)
	if err != nil {
		return nil, err
	}
	return nil, phone.Changed(ctx, seen, timeout)
}

func uiSetText(ctx context.Context, phone *device.Phone, call *operation) error {
	var ref, text string
	var timeoutMs *float64
	if err := call.decode(2, &ref, &text, &timeoutMs); err != nil {
		return err
	}
	timeout, err := elementTimeout(timeoutMs)
	if err != nil {
		return err
	}
	return phone.SetText(ctx, ref, text, timeout)
}

func uiPerform(ctx context.Context, phone *device.Phone, call *operation) error {
	var ref, name string
	if err := call.decode(2, &ref, &name); err != nil {
		return err
	}
	return phone.Perform(ctx, ref, name)
}

func uiTap(ctx context.Context, phone *device.Phone, call *operation) error {
	var ref string
	var timeoutMs *float64
	if err := call.decode(1, &ref, &timeoutMs); err != nil {
		return err
	}
	timeout, err := elementTimeout(timeoutMs)
	if err != nil {
		return err
	}
	return phone.Tap(ctx, ref, timeout)
}

func elementTimeout(timeoutMs *float64) (time.Duration, error) {
	if timeoutMs == nil {
		return -1, nil
	}
	if *timeoutMs < 0 {
		return 0, E.Cause1(ErrInvalid, E.New("timeoutMs must not be negative"))
	}
	return milliseconds(*timeoutMs), nil
}

func watchDeviceState(ctx context.Context, call *operation, publish func(any), ended func(error)) error {
	var name string
	if err := call.decode(1, &name); err != nil {
		return err
	}
	phone := call.phone()
	switch name {
	case "foreground":
		go func() {
			ended(phone.WatchForeground(ctx, func(app string) { publish(app) }))
		}()
	case "locked", "display":
		go func() {
			ended(phone.WatchState(ctx, name, publish))
		}()
	case "simulatedLocation":
		go call.engine.host.options.Device.location.watch(ctx, publish)
	default:
		return E.Cause1(ErrInvalid, E.New("unknown state ", strconv.Quote(name)))
	}
	return nil
}

func watchApplicationEvents(ctx context.Context, call *operation, publish func(any), ended func(error)) error {
	if err := call.decode(0); err != nil {
		return err
	}
	phone := call.phone()
	go func() {
		ended(phone.WatchApps(ctx, func(event device.AppEvent) {
			publish(map[string]any{"type": event.Kind, "bundleId": event.BundleID, "pid": event.PID, "time": event.Time.UnixMilli()})
		}))
	}()
	return nil
}

type strict struct {
	value any
}

func (o *strict) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return E.New("expected an options object")
	}
	structType := reflect.TypeOf(o.value).Elem()
	names := make([]string, structType.NumField())
	for index := range names {
		names[index], _, _ = strings.Cut(structType.Field(index).Tag.Get("json"), ",")
	}
	for name, value := range fields {
		index := slices.Index(names, name)
		if index < 0 {
			return E.New("unknown option ", strconv.Quote(name), "; the options are ", strings.Join(names, ", "))
		}
		if string(value) == "null" {
			return E.New("option ", name, " is null")
		}
	}
	return json.Unmarshal(data, o.value)
}

func duration(name string, value *float64) (time.Duration, error) {
	if value == nil {
		return 0, nil
	}
	if *value <= 0 {
		return 0, E.Cause1(ErrInvalid, E.New(name, " must be positive"))
	}
	return milliseconds(*value), nil
}

func milliseconds(value float64) time.Duration {
	return time.Duration(value * float64(time.Millisecond))
}
