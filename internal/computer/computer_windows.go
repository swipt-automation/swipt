//go:build cgo

package computer

import (
	"net"
	"syscall"

	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/sys/windows"
)

const soExclusiveAddrUse = ^windows.SO_REUSEADDR

var (
	errPortTaken = []error{windows.WSAEADDRINUSE, windows.WSAEACCES}
	listenConfig = net.ListenConfig{Control: func(_, _ string, conn syscall.RawConn) error {
		var err error
		controlErr := conn.Control(func(fd uintptr) {
			err = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, soExclusiveAddrUse, 1)
		})
		return E.Errors(controlErr, err)
	}}
)
