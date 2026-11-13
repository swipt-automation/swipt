package display

import (
	"context"
	"math"
	"strconv"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/version"
)

// pymobiledevice3 sends getdisplayinfo to deviceinfo with no action
// identifier (pymobiledevice3 1146717,
// pymobiledevice3/remote/core_device/device_info.py:12, 23-27).
const (
	deviceInfoServiceName = "com.apple.coredevice.deviceinfo"
	featureDisplayInfo    = "com.apple.coredevice.feature.getdisplayinfo"
)

type DisplayInfo struct {
	ID                        int
	UniqueID                  string
	Primary                   bool
	Integrated                bool
	Backlight                 string
	Width, Height             int
	NativeWidth, NativeHeight int
	StreamWidth, StreamHeight int
	CurrentOrientation        string
	PointScale                int
}

func (c *Client) read(ctx context.Context) (map[string]any, error) {
	ctx, cancel := c.operationContext(ctx)
	defer cancel()
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	conn, err := c.device.DialXPC(ctx, deviceInfoServiceName)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	return coredevice.InvokeFeature(ctx, conn, c.deviceIdentifier, featureDisplayInfo, "", map[string]any{})
}

func (c *Client) Displays(ctx context.Context) ([]DisplayInfo, error) {
	output, err := c.read(ctx)
	if err != nil {
		return nil, err
	}
	return displays(output), nil
}

func displays(output map[string]any) []DisplayInfo {
	listed := common.Map(array(output["displays"]), func(value any) DisplayInfo {
		display := dictionary(value)
		info := DisplayInfo{
			ID:                 intField(display, "displayId"),
			UniqueID:           stringValue(display["uniqueId"]),
			Backlight:          stringValue(display["backlightState"]),
			CurrentOrientation: stringValue(display["currentOrientation"]),
			PointScale:         intField(display, "pointScale"),
		}
		info.Primary, _ = boolValue(display["primary"])
		external, _ := boolValue(display["external"])
		_, integrated := dictionary(display["type"])["integrated"]
		info.Integrated = integrated && !external
		info.Width, info.Height = sizeValue(dictionary(display["currentMode"])["size"])
		info.NativeWidth, info.NativeHeight = sizeValue(display["nativeSize"])
		return info
	})
	primary := primaryOf(listed)
	// A video stream carries a display larger than the primary one scaled down
	// to the primary display's width or height, with the other side rounded up
	// to an even number: the 2007x2853 inner display of an iPhone Duo arrives as
	// 1398x1988 beside its 1398x2034 cover display (iPhone19,4 27.2 24B91020).
	for index, info := range listed {
		info.StreamWidth, info.StreamHeight = info.Width, info.Height
		if primary.Width > 0 && (info.Width > primary.Width || info.Height > primary.Height) {
			if info.Width*primary.Height >= info.Height*primary.Width {
				info.StreamWidth = primary.Width
				info.StreamHeight = even(float64(info.Height) * float64(primary.Width) / float64(info.Width))
			} else {
				info.StreamWidth = even(float64(info.Width) * float64(primary.Height) / float64(info.Height))
				info.StreamHeight = primary.Height
			}
		}
		listed[index] = info
	}
	return listed
}

func even(length float64) int {
	return int(math.Ceil(length/2)) * 2
}

func primaryOf(displays []DisplayInfo) DisplayInfo {
	primary := common.Find(displays, func(info DisplayInfo) bool {
		return info.Primary && info.Width > 0 && info.Height > 0
	})
	if primary.Width == 0 {
		primary = common.Find(displays, func(info DisplayInfo) bool {
			return info.ID == defaultDisplayID && info.Width > 0 && info.Height > 0
		})
	}
	return primary
}

// An iPhone Duo lists its cover display as primary and its inner display as a
// second integrated one. Folded, the cover's backlightState is activeOn and the
// inner display's is off; unfolded, the cover's is off and the inner display's
// is activeOn (iPhone19,4 27.2 24B91020).
func Active(displays []DisplayInfo) (DisplayInfo, bool) {
	primary := primaryOf(displays)
	if primary.lit() {
		return primary, true
	}
	lit := common.Find(displays, func(info DisplayInfo) bool {
		return info.Integrated && info.lit() && info.Width > 0 && info.Height > 0
	})
	if lit.Width > 0 {
		return lit, true
	}
	return primary, primary.Width > 0
}

func (i DisplayInfo) lit() bool {
	switch i.Backlight {
	case "inactiveOn", "activeOn", "activeDimmed":
		return true
	}
	return false
}

func (c *Client) BacklightState(ctx context.Context) (string, error) {
	output, err := c.read(ctx)
	if err != nil {
		return "", err
	}
	active, found := Active(displays(output))
	if !found {
		return "", E.New("the device lists no usable screen")
	}
	// CoreDeviceUtilities names BLSBacklightState 3 activeDimmed, and answers
	// unknown for any other state or a display it has no backlight of (DDI
	// 27B5028f CoreDeviceUtilities 0x37298c, 0x35aecc, 0x372bbc).
	state := active.Backlight
	// It reads a display's backlight only through BLSDisplayReference, and answers
	// unknown for every display where BacklightServices has no such class, as on
	// iOS 26.6.2 23G90; the aggregate state comes from BLSBacklight.sharedBacklight
	// (DDI 27B5028f CoreDeviceUtilities 0x372a04, 0x37206c).
	current, knownVersion := version.Parse(c.device.Info().OSVersion)
	if aggregate, _ := output["backlightState"].(string); knownVersion && !current.AtLeast(version.Version{27}) && state == "unknown" && aggregate != "" {
		state = aggregate
	}
	switch state {
	case "off", "inactiveOn", "activeOn":
		return state, nil
	case "activeDimmed":
		return "dimmed", nil
	case "":
		return "", E.New("the device did not report its screen's backlight state")
	}
	return "", E.New("the device reported the backlight state ", strconv.Quote(state), " for its screen")
}

func sizeValue(value any) (int, int) {
	size := array(value)
	if len(size) != 2 {
		return 0, 0
	}
	width, _ := floatValue(size[0])
	height, _ := floatValue(size[1])
	return int(width), int(height)
}
