//go:build cgo

package computer

import (
	"archive/zip"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

func zipApplication(target io.Writer, application string) error {
	writer := zip.NewWriter(target)
	root := path.Join("Payload", filepath.Base(application))
	err := filepath.WalkDir(application, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(application, file)
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = path.Join(root, filepath.ToSlash(relative))
		switch {
		case entry.IsDir():
			header.Name += "/"
			_, err = writer.CreateHeader(header)
			return err
		case entry.Type()&fs.ModeSymlink != 0:
			destination, err := os.Readlink(file)
			if err != nil {
				return err
			}
			header.Method = zip.Deflate
			output, err := writer.CreateHeader(header)
			if err != nil {
				return err
			}
			_, err = io.WriteString(output, filepath.ToSlash(destination))
			return err
		}
		input, err := os.Open(file)
		if err != nil {
			return err
		}
		defer input.Close()
		header.Method = zip.Deflate
		output, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = io.Copy(output, input)
		return err
	})
	if err != nil {
		return err
	}
	return writer.Close()
}
