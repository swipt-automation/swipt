package housearrest

import (
	"fmt"
	"net"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/rsd"
)

const Service = "com.apple.mobile.house_arrest"

// go-ios speaks AFC on the connection once VendContainer answers Complete,
// and calls what it reaches the full app container (go-ios 63c0d57,
// ios/house_arrest/house_arrest.go:30, 42-44, 98-100).
func VendContainer(conn net.Conn, bundleID string) error {
	plist := rsd.NewPlistConn(conn)
	err := plist.Send(map[string]any{"Command": "VendContainer", "Identifier": bundleID})
	if err != nil {
		return E.Cause(err, "house_arrest: open the container of ", bundleID)
	}
	reply, err := plist.Receive()
	if err != nil {
		return E.Cause(err, "house_arrest: open the container of ", bundleID)
	}
	if reply["Status"] != "Complete" {
		return E.New("house_arrest: open the container of ", bundleID, ": ", fmt.Sprint(reply["Error"]))
	}
	return nil
}
