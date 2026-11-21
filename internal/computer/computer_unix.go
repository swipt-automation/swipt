//go:build !android && cgo && !windows

package computer

import (
	"net"
	"syscall"
)

var (
	errPortTaken = []error{syscall.EADDRINUSE}
	listenConfig net.ListenConfig
)
