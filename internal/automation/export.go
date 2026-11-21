//go:build cgo

package automation

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	E "github.com/sagernet/sing/common/exceptions"
)

func (h *Host) Export(id string, output io.Writer) error {
	swiplet, err := h.Swiplet(id)
	if err != nil {
		return err
	}
	names := []string{ManifestFile}
	if _, err = os.Stat(filepath.Join(swiplet.Directory, IconFile)); err == nil {
		names = append(names, IconFile)
	}
	for _, folder := range []string{SourceFolder, AssetsFolder} {
		if _, err = os.Stat(filepath.Join(swiplet.Directory, folder)); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		err = fs.WalkDir(os.DirFS(swiplet.Directory), folder, func(name string, item fs.DirEntry, err error) error {
			if err == nil && item.Type().IsRegular() {
				names = append(names, name)
			}
			return err
		})
		if err != nil {
			return err
		}
	}
	writer := zip.NewWriter(output)
	for _, name := range names {
		if err = addToArchive(writer, swiplet.Directory, name); err != nil {
			return E.Errors(err, writer.Close())
		}
	}
	return writer.Close()
}

func addToArchive(writer *zip.Writer, directory string, name string) error {
	file, err := os.Open(filepath.Join(directory, name))
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name, header.Method = name, zip.Deflate
	output, err := writer.CreateHeader(header)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, file)
	return err
}
