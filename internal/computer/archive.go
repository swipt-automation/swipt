//go:build cgo

package computer

import (
	"archive/zip"
	"debug/macho"
	"encoding/binary"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
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
		head := make([]byte, 4)
		length, err := io.ReadFull(input, head)
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
			return err
		}
		head = head[:length]
		header.Method = zip.Deflate
		if length == 4 && (slices.Contains([]uint32{macho.Magic32, macho.Magic64}, binary.LittleEndian.Uint32(head)) || binary.BigEndian.Uint32(head) == macho.MagicFat) {
			header.SetMode(info.Mode() | 0o111)
		}
		output, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err = output.Write(head); err != nil {
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

func Unzip(archive, directory string) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer reader.Close()
	if err = os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		return err
	}
	defer root.Close()
	links := common.Filter(reader.File, func(it *zip.File) bool { return it.Mode()&fs.ModeSymlink != 0 })
	others := common.Filter(reader.File, func(it *zip.File) bool { return it.Mode()&fs.ModeSymlink == 0 })
	for _, entry := range append(others, links...) {
		if err = extract(root, entry); err != nil {
			return E.Cause(err, "extract ", entry.Name)
		}
	}
	resolvedRoot, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return err
	}
	for _, entry := range links {
		resolved, err := filepath.EvalSymlinks(filepath.Join(directory, filepath.FromSlash(entry.Name)))
		if err != nil {
			return E.Cause(err, "resolve ", entry.Name)
		}
		if relative, err := filepath.Rel(resolvedRoot, resolved); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return E.New(archive, ": ", entry.Name, " leads outside the archive")
		}
	}
	return nil
}

func extract(root *os.Root, entry *zip.File) error {
	name, mode := filepath.FromSlash(entry.Name), entry.Mode()
	if mode.IsDir() {
		return root.MkdirAll(name, 0o755)
	}
	if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		return err
	}
	input, err := entry.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	if mode&fs.ModeSymlink != 0 {
		destination, err := io.ReadAll(input)
		if err != nil {
			return err
		}
		return root.Symlink(string(destination), name)
	}
	output, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm()|0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, input)
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	return err
}
