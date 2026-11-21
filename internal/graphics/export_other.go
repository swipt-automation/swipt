//go:build cgo && !darwin && !android

package graphics

import (
	"io"
	"os"
	"path/filepath"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
)

func (r *screenRecording) export(_ *automation.GraphicsCall, target string, mp4 bool) error {
	if len(r.manifest.Segments) > 1 {
		return unsupported("exporting a recording whose screen turned")
	}
	file := r.manifest.Segments[0].File
	source, err := os.Open(filepath.Join(r.directory, file))
	if err != nil {
		return failure(nameData, fileFailure(err, "graphics: read ", file))
	}
	defer source.Close()
	output, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, source)
	if err == nil && mp4 {
		_, err = output.WriteAt(fileType(true), 0)
	}
	return E.Errors(err, output.Close())
}
