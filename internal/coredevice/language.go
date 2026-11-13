package coredevice

import (
	"context"
	"fmt"

	E "github.com/sagernet/sing/common/exceptions"
)

// Lockdown reports only the first of the preferred languages (iOS 27.2
// 24B115).
func (d *Device) Language(ctx context.Context) (string, error) {
	conn, err := d.DialShim(ctx, remoteLockdownService)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	reply, err := conn.Request(ctx, map[string]any{
		"Label":           checkinLabel,
		"ProtocolVersion": "2",
		"Request":         "GetValue",
		"Domain":          "com.apple.international",
		"Key":             "Language",
	})
	if err != nil {
		return "", E.Cause(err, "read the device's language")
	}
	if message, failed := reply["Error"]; failed {
		return "", E.New("read the device's language: ", fmt.Sprint(message))
	}
	language, ok := reply["Value"].(string)
	if !ok || language == "" {
		return "", E.New("the device did not report its language")
	}
	return language, nil
}
