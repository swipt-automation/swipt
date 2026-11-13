//go:build cgo

package computer

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/swipt-automation/swipt/internal/log"
)

const diskImageFolder = "DeveloperDiskImage"

func diskImageDirectory(given string, logger log.ContextLogger) string {
	cacheRoot, err := os.UserCacheDir()
	cached := filepath.Join(cacheRoot, swiptFolder, diskImageFolder)
	if given != "" {
		if err == nil {
			err = keepDiskImage(given, cached)
		}
		if err != nil {
			logger.Debug("keep a copy of the Developer Disk Image: ", err)
		}
		return given
	}
	for _, directory := range []string{systemDiskImages, cached} {
		if _, statErr := os.Stat(directory); directory != "" && statErr == nil {
			return directory
		}
	}
	return ""
}

type fileStamp struct {
	path     string
	size     int64
	modified time.Time
}

func stamps(root string) ([]fileStamp, error) {
	var result []fileStamp
	err := filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.Type().IsRegular() {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, file)
		result = append(result, fileStamp{path: relative, size: info.Size(), modified: info.ModTime()})
		return err
	})
	return result, err
}

func keepDiskImage(source, target string) error {
	given, err := stamps(source)
	if err != nil {
		return err
	}
	kept, _ := stamps(target)
	if slices.EqualFunc(given, kept, func(a, b fileStamp) bool {
		return a.path == b.path && a.size == b.size && a.modified.Equal(b.modified)
	}) {
		return nil
	}
	staging := target + ".partial"
	if err = os.RemoveAll(staging); err != nil {
		return err
	}
	for _, it := range given {
		if err = copyFile(filepath.Join(source, it.path), filepath.Join(staging, it.path), it.modified); err != nil {
			return err
		}
	}
	if err = os.RemoveAll(target); err != nil {
		return err
	}
	return os.Rename(staging, target)
}

func copyFile(source, target string, modified time.Time) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err = os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	output, err := os.Create(target)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Chtimes(target, modified, modified)
}
