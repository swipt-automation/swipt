package atomicfile

import (
	"io/fs"
	"os"
	"path/filepath"

	E "github.com/sagernet/sing/common/exceptions"
)

func Write(path string, data []byte, mode fs.FileMode) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".write-*")
	if err != nil {
		return err
	}
	err = file.Chmod(mode)
	if err == nil {
		_, err = file.Write(data)
	}
	if err == nil {
		err = file.Sync()
	}
	err = E.Errors(err, file.Close())
	if err == nil {
		err = os.Rename(file.Name(), path)
	}
	if err != nil {
		_ = os.Remove(file.Name())
	}
	return err
}
