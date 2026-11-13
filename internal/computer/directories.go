//go:build cgo

package computer

import (
	"cmp"
	"os"
	"path/filepath"
	"runtime"
)

const swiptFolder = "swipt"

func userDirectories() (configuration string, caches string, err error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		configuration = cmp.Or(os.Getenv("XDG_CONFIG_HOME"), filepath.Join(home, ".config"))
		caches = cmp.Or(os.Getenv("XDG_CACHE_HOME"), filepath.Join(home, ".cache"))
	} else {
		configuration, err = os.UserConfigDir()
		if err != nil {
			return "", "", err
		}
		caches, err = os.UserCacheDir()
		if err != nil {
			return "", "", err
		}
	}
	return filepath.Join(configuration, swiptFolder), filepath.Join(caches, swiptFolder), nil
}
