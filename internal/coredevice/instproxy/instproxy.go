package instproxy

import (
	"fmt"
	"net"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/rsd"
)

const (
	Service = "com.apple.mobile.installation_proxy"

	statusComplete = "Complete"
)

// A command answers with status messages until Status is Complete or Error is
// set (go-ios 6910f3b, ios/installationproxy/installationproxy.go:111, 169-173).
func run(conn net.Conn, command map[string]any) error {
	plist := rsd.NewPlistConn(conn)
	err := plist.Send(command)
	if err != nil {
		return err
	}
	for {
		reply, err := plist.Receive()
		if err != nil {
			return err
		}
		if failure, failed := reply["Error"]; failed {
			return E.New(fmt.Sprint(failure), ": ", fmt.Sprint(reply["ErrorDescription"]))
		}
		if reply["Status"] == statusComplete {
			return nil
		}
	}
}

func Install(conn net.Conn, packagePath, bundleID string) error {
	err := run(conn, map[string]any{
		"Command":       "Install",
		"PackagePath":   packagePath,
		"ClientOptions": map[string]any{"CFBundleIdentifier": bundleID, "PackageType": "Developer"},
	})
	if err != nil {
		return E.Cause(err, "instproxy: install ", bundleID)
	}
	return nil
}

type Application struct {
	BundleID      string
	Name          string
	Path          string
	Version       string
	BundleVersion string
}

// Browse answers with chunks of CurrentList until Status is Complete (go-ios
// 63c0d57, ios/installationproxy/installationproxy.go:88-131, 194-203).
func Browse(conn net.Conn) ([]Application, error) {
	plist := rsd.NewPlistConn(conn)
	err := plist.Send(map[string]any{
		"Command": "Browse",
		"ClientOptions": map[string]any{
			"ApplicationType":  "User",
			"ReturnAttributes": []any{"CFBundleIdentifier", "CFBundleDisplayName", "CFBundleName", "CFBundleShortVersionString", "CFBundleVersion", "Path"},
		},
	})
	if err != nil {
		return nil, E.Cause(err, "instproxy: browse")
	}
	var applications []Application
	for {
		reply, err := plist.Receive()
		if err != nil {
			return nil, E.Cause(err, "instproxy: browse")
		}
		if failure, failed := reply["Error"]; failed {
			return nil, E.New("instproxy: browse: ", fmt.Sprint(failure), ": ", fmt.Sprint(reply["ErrorDescription"]))
		}
		list, _ := reply["CurrentList"].([]any)
		for _, item := range list {
			attributes, _ := item.(map[string]any)
			text := func(key string) string {
				value, _ := attributes[key].(string)
				return value
			}
			name := text("CFBundleDisplayName")
			if name == "" {
				name = text("CFBundleName")
			}
			applications = append(applications, Application{
				BundleID: text("CFBundleIdentifier"), Name: name, Path: text("Path"),
				Version: text("CFBundleShortVersionString"), BundleVersion: text("CFBundleVersion"),
			})
		}
		if reply["Status"] == statusComplete {
			return applications, nil
		}
	}
}

func Uninstall(conn net.Conn, bundleID string) error {
	err := run(conn, map[string]any{
		"Command":               "Uninstall",
		"ApplicationIdentifier": bundleID,
		"ClientOptions":         map[string]any{},
	})
	if err != nil {
		return E.Cause(err, "instproxy: uninstall ", bundleID)
	}
	return nil
}
