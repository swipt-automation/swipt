package springboard

import (
	"context"
	"fmt"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
)

// pymobiledevice3 reaches SpringBoard services over RSD through this shim and
// reads the icon from pngData of the getIconPNGData reply (pymobiledevice3
// 9537c0a, pymobiledevice3/services/springboard.py:25, 51-61). An unknown or
// empty bundle ID gets the generic app icon instead of an error (iOS 27.2
// 24B115).
const serviceName = "com.apple.springboardservices.shim.remote"

func IconPNG(ctx context.Context, device *coredevice.Device, bundleID string) ([]byte, error) {
	conn, err := device.DialShim(ctx, serviceName)
	if err != nil {
		return nil, E.Cause(err, "springboard: read the icon of ", bundleID)
	}
	defer conn.Close()
	reply, err := conn.Request(ctx, map[string]any{"command": "getIconPNGData", "bundleId": bundleID})
	if err != nil {
		return nil, E.Cause(err, "springboard: read the icon of ", bundleID)
	}
	if message, failed := reply["Error"]; failed {
		return nil, E.New("springboard: read the icon of ", bundleID, ": ", fmt.Sprint(message))
	}
	data, _ := reply["pngData"].([]byte)
	if len(data) == 0 {
		return nil, E.New("springboard: no icon for ", bundleID)
	}
	return data, nil
}
