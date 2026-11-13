package coredevice

import (
	"context"
	"fmt"

	E "github.com/sagernet/sing/common/exceptions"
)

// Lockdown reports only the first of the preferred languages (iPhone18,2, iOS
// 27.2 24B115).
func (d *Device) Language(ctx context.Context) (string, error) {
	return d.lockdownString(ctx, "com.apple.international", "Language", "language")
}

func (d *Device) TimeZone(ctx context.Context) (string, error) {
	return d.lockdownString(ctx, "", "TimeZone", "time zone")
}

func (d *Device) lockdownString(ctx context.Context, domain string, key string, description string) (string, error) {
	conn, err := d.DialShim(ctx, remoteLockdownService)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	request := map[string]any{
		"Label":           checkinLabel,
		"ProtocolVersion": "2",
		"Request":         "GetValue",
		"Key":             key,
	}
	if domain != "" {
		request["Domain"] = domain
	}
	reply, err := conn.Request(ctx, request)
	if err != nil {
		return "", E.Cause(err, "read the device's ", description)
	}
	if message, failed := reply["Error"]; failed {
		return "", E.New("read the device's ", description, ": ", fmt.Sprint(message))
	}
	value, ok := reply["Value"].(string)
	if !ok || value == "" {
		return "", E.New("the device did not report its ", description)
	}
	return value, nil
}
