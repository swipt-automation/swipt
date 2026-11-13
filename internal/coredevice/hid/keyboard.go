package hid

import (
	"context"
	"maps"
	"slices"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/xpc"
)

// The createService XPC types match idevice: PrimaryUsage, PrimaryUsagePage
// and _ServiceID are uint64 at the top level, while the Codable storage
// repeats properties in typed dictionaries (idevice d32c818,
// idevice/src/services/core_device/hid.rs:142-146, 276-321).
const (
	keyboardUsagePage = 0x01
	keyboardUsage     = 0x06
	keyboardVendorID  = 0x05ac
	keyboardProductID = 0x0250
	// pymobiledevice3 uses 0x100002001 as its default virtual keyboard
	// service ID (pymobiledevice3 1146717,
	// pymobiledevice3/remote/core_device/hid_service.py:123, 445).
	keyboardServiceID = 0x1_0000_2001

	keyboardProduct      = "Swipt Keyboard"
	keyboardManufacturer = "Swipt"
)

// backboardd attaches a keyboard of usage page 1 and usage 6 as a hardware
// keyboard (24B5110a backboardd 0x100031ab0, 0x100033c0c), for which UIKit
// minimizes the software keyboard as far as the scene, automatic minimization
// and the input mode's showSWLayoutWithHWKeyboard allow (24A446 UIKitCore
// 0x184921c98, 0x1850144b0). CoreDevice publishes its own keyboard with usage
// page and usage 0 instead (DDI 27B5028f CoreDeviceUtilities 0x2a7718,
// 0x2a741c).
type KeyboardMode int

const (
	HardwareKeyboard KeyboardMode = iota
	SoftwareKeyboard
)

func createKeyboardPayload(mode KeyboardMode) map[string]any {
	page, usage := int64(keyboardUsagePage), int64(keyboardUsage)
	if mode == SoftwareKeyboard {
		page, usage = 0, 0
	}
	storage := map[string]any{
		"Manufacturer":     map[string]any{"string": keyboardManufacturer},
		"Product":          map[string]any{"string": keyboardProduct},
		"ProductID":        map[string]any{"int": int64(keyboardProductID)},
		"VendorID":         map[string]any{"int": int64(keyboardVendorID)},
		"PrimaryUsage":     map[string]any{"int": usage},
		"PrimaryUsagePage": map[string]any{"int": page},
		"DeviceUsagePairs": map[string]any{"array": []any{
			map[string]any{"dictionary": map[string]any{
				"DeviceUsage":     map[string]any{"int": usage},
				"DeviceUsagePage": map[string]any{"int": page},
			}},
		}},
		"Transport":                      map[string]any{"string": "USB"},
		"ReportDescriptor":               map[string]any{"data": keyboardReportDescriptor},
		"UniversalControlVirtualService": map[string]any{"bool": true},
		"_ServiceID":                     map[string]any{"uint": uint64(keyboardServiceID)},
	}
	return map[string]any{"createService": map[string]any{"_0": map[string]any{
		"DeviceUsagePairs": []any{map[string]any{
			"DeviceUsage":     usage,
			"DeviceUsagePage": page,
		}},
		"PrimaryUsage":                       uint64(usage),
		"PrimaryUsagePage":                   uint64(page),
		"Product":                            keyboardProduct,
		"ProductID":                          int64(keyboardProductID),
		"VendorID":                           int64(keyboardVendorID),
		"_CoreDevice_codablePropertyStorage": storage,
		"_ServiceID":                         uint64(keyboardServiceID),
	}}}
}

func (c *Client) PrepareKeyboard(ctx context.Context) error {
	if c.keyboard != 0 {
		return nil
	}
	conn, err := c.universalConn(ctx)
	if err != nil {
		return err
	}
	reply, err := conn.Request(ctx, universalRequest(createKeyboardPayload(c.keyboardMode)))
	if err == nil && ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		c.closeUniversal()
		return E.Cause(err, "hid: create keyboard")
	}
	serviceID, ok := uintValue(reply["serviceID"])
	if !ok || serviceID == 0 {
		return E.New("hid: create keyboard: reply without a service ID: ", strings.Join(slices.Sorted(maps.Keys(reply)), ", "))
	}
	c.keyboard = serviceID
	c.logger.Debug("created keyboard, service ", serviceID)
	return nil
}

func (c *Client) removeKeyboard(ctx context.Context) error {
	if c.keyboard == 0 {
		return nil
	}
	if err := removeService(ctx, c.universal, c.keyboard); err != nil {
		c.closeUniversal()
		return E.Cause(err, "hid: remove keyboard")
	}
	c.keyboard = 0
	c.pressed = [keyboardBitmapSize]byte{}
	return nil
}

// idevice sends resetGestureState before removeService, without reply
// flags (idevice d32c818, idevice/src/services/core_device/hid.rs:886-891, 987-997).
func removeService(ctx context.Context, conn *xpc.Conn, serviceID uint64) error {
	for _, call := range []string{"resetGestureState", "removeService"} {
		err := conn.SendContext(ctx, universalRequest(map[string]any{
			call: map[string]any{"_0": serviceID},
		}), false)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) KeyDown(ctx context.Context, usage uint8) error {
	return c.changeKey(ctx, usage, true)
}

func (c *Client) KeyUp(ctx context.Context, usage uint8) error {
	return c.changeKey(ctx, usage, false)
}

func (c *Client) changeKey(ctx context.Context, usage uint8, down bool) error {
	pressed := c.pressed
	if down {
		pressed[usage/8] |= 1 << (usage % 8)
	} else {
		pressed[usage/8] &^= 1 << (usage % 8)
	}
	return c.sendKeys(ctx, pressed)
}

// SpringBoard records each key down from an external keyboard by usage and
// sender ID, and drops it on the up from the same sender. A key down while
// the screen is off, other than the consumer play/pause, mute, volume and
// power keys, makes SpringBoard swallow every external keyboard key it
// receives, such as system shortcuts and brightness keys, until a key up
// leaves that record empty (24B5110a SpringBoard -[SpringBoard
// _shouldSwallowHIDEvent:] 0x226a2c448..0x226a2c854, called from -[SpringBoard
// handleKeyHIDEvent:] and -[SpringBoard __handleHIDEvent:withUIEvent:]).
func (c *Client) releaseKeys(ctx context.Context) error {
	if c.keyboard == 0 || c.pressed == [keyboardBitmapSize]byte{} {
		return nil
	}
	return c.sendKeys(ctx, [keyboardBitmapSize]byte{})
}

func (c *Client) sendKeys(ctx context.Context, pressed [keyboardBitmapSize]byte) error {
	if err := c.PrepareKeyboard(ctx); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := c.sendReport(c.ctx, c.keyboard, keyboardReport(&pressed, now())); err != nil {
		return err
	}
	c.pressed = pressed
	return nil
}
