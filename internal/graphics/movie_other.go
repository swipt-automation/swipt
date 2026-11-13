//go:build cgo && !darwin

package graphics

import "os"

func rebrandMP4(file *os.File) error {
	_, err := file.WriteAt(fileType(true), 0)
	return err
}
