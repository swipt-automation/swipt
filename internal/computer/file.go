//go:build cgo

package computer

import (
	"os"
	"path/filepath"

	E "github.com/sagernet/sing/common/exceptions"
)

func writeFileAtomically(file string, data []byte) error {
	partial, err := os.CreateTemp(filepath.Dir(file), filepath.Base(file)+".*")
	if err != nil {
		return err
	}
	_, err = partial.Write(data)
	if err == nil {
		err = partial.Sync()
	}
	err = E.Errors(err, partial.Close())
	if err == nil {
		err = os.Rename(partial.Name(), file)
	}
	if err != nil {
		_ = os.Remove(partial.Name())
	}
	return err
}
