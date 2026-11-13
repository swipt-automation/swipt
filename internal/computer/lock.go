//go:build cgo

package computer

import (
	"errors"
	"os"

	E "github.com/sagernet/sing/common/exceptions"
)

func acquire(path string, url string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err = lockFile(file); err != nil {
		_ = file.Close()
		if !errors.Is(err, errLocked) {
			return nil, E.Cause(err, "lock ", path)
		}
		return nil, E.New(`swipt-driver is already running on this computer; stop it with "swipt device stop -d `, url, `"`)
	}
	return file, nil
}
