//go:build android

package android

import (
	"context"
	"sync"
	"sync/atomic"
)

var (
	lastCall      atomic.Int64
	canceledCalls sync.Map
)

func CallCanceled(call int64) bool {
	_, canceled := canceledCalls.Load(call)
	return canceled
}

func (d *Device) run(ctx context.Context, call func(id int64) error) error {
	id := lastCall.Add(1)
	interrupted := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		canceledCalls.Store(id, struct{}{})
		d.Layer.Interrupt(id)
		close(interrupted)
	})
	err := call(id)
	if !stop() {
		<-interrupted
	}
	canceledCalls.Delete(id)
	return Failure(err)
}

func (d *Device) Lock(ctx context.Context) error {
	return d.run(ctx, d.Layer.Lock)
}

func (d *Device) LaunchApplication(ctx context.Context, packageName string, data string, restart bool, display int32) error {
	return d.run(ctx, func(id int64) error { return d.Layer.LaunchApplication(id, packageName, data, restart, display) })
}

func (d *Device) Touch(ctx context.Context, display int32, fingers string) error {
	return d.run(ctx, func(id int64) error { return d.Layer.Touch(id, display, fingers) })
}

func (d *Device) AccessibilitySnapshot(ctx context.Context, display int32) (string, error) {
	var snapshot string
	err := d.run(ctx, func(id int64) (err error) {
		snapshot, err = d.Layer.AccessibilitySnapshot(id, display)
		return
	})
	return snapshot, err
}

func (d *Device) WaitAccessibilityChange(ctx context.Context, seen int64, timeoutMs int64) error {
	return d.run(ctx, func(id int64) error { return d.Layer.WaitAccessibilityChange(id, seen, timeoutMs) })
}

func (d *Device) WaitAccessibilityIdle(ctx context.Context, display int32, idleMs int64, timeoutMs int64) (bool, error) {
	var idle bool
	err := d.run(ctx, func(id int64) (err error) {
		idle, err = d.Layer.WaitAccessibilityIdle(id, display, idleMs, timeoutMs)
		return
	})
	return idle, err
}

func (d *Device) AimElement(ctx context.Context, element string) (string, error) {
	var aim string
	err := d.run(ctx, func(id int64) (err error) {
		aim, err = d.Layer.AimElement(id, element)
		return
	})
	return aim, err
}

func (d *Device) SetElementText(ctx context.Context, element string, text string) (string, error) {
	var pending string
	err := d.run(ctx, func(id int64) (err error) {
		pending, err = d.Layer.SetElementText(id, element, text)
		return
	})
	return pending, err
}

func (d *Device) PerformElementAction(ctx context.Context, element string, action string, arguments string) error {
	return d.run(ctx, func(id int64) error { return d.Layer.PerformElementAction(id, element, action, arguments) })
}
