//go:build cgo

package computer

import (
	"os"

	"golang.org/x/sys/windows"
)

var errLocked = windows.ERROR_LOCK_VIOLATION

func lockFile(file *os.File) error {
	return windows.LockFileEx(windows.Handle(file.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, new(windows.Overlapped))
}
