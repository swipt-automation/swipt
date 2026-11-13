//go:build cgo && !windows

package computer

import (
	"os"

	"golang.org/x/sys/unix"
)

var errLocked = unix.EWOULDBLOCK

func lockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
