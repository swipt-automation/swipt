//go:build cgo && !darwin

package graphics

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
)

type screenRecording struct {
	directory string
	metadata  recordingMetadata
}

func openRecording(directory string) (*screenRecording, error) {
	data, err := os.ReadFile(filepath.Join(directory, recordingManifest))
	if err != nil {
		return nil, failure(nameNotFound, "graphics: ", filepath.Base(directory), " has no ", recordingManifest)
	}
	var metadata recordingMetadata
	if err = json.Unmarshal(data, &metadata); err != nil {
		return nil, failure(nameData, "graphics: read ", recordingManifest, ": ", err.Error())
	}
	var segments []recordingSegment
	for _, segment := range metadata.Segments {
		if segment.File == "" || segment.File == "." || segment.File == ".." || strings.ContainsAny(segment.File, `/\`) {
			continue
		}
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		return nil, failure(nameData, "graphics: ", recordingManifest, " lists no video")
	}
	metadata.Segments = segments
	return &screenRecording{directory: directory, metadata: metadata}, nil
}

func (r *screenRecording) info() string {
	encoded, _ := json.Marshal(map[string]float64{"start": rounded(r.metadata.Start), "duration": rounded(r.metadata.Duration)})
	return string(encoded)
}

func (r *screenRecording) export(target string, mp4 bool) error {
	if len(r.metadata.Segments) > 1 {
		return failure(nameNotSupported, "graphics: exporting a recording whose screen turned is not available on this platform")
	}
	source, err := os.Open(filepath.Join(r.directory, r.metadata.Segments[0].File))
	if err != nil {
		if pathError, isPath := errors.AsType[*fs.PathError](err); isPath {
			err = pathError.Err
		}
		return failure(nameData, "graphics: read ", r.metadata.Segments[0].File, ": ", err)
	}
	defer source.Close()
	output, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, source)
	if err == nil && mp4 {
		err = rebrand(output)
	}
	return E.Errors(err, output.Close())
}

var exportBrands = []string{"isom", "iso6", "mp41", "hvc1"}

func rebrand(file *os.File) error {
	header := make([]byte, 8)
	if _, err := file.ReadAt(header, 0); err != nil {
		return E.Cause(err, "graphics: read the movie")
	}
	size := int(binary.BigEndian.Uint32(header))
	if string(header[4:]) != "ftyp" || size < 16 || (size-16)%4 != 0 {
		return E.New("graphics: the movie has no file type to change")
	}
	brands := pack("isom", uint32(0x200))
	for index := range (size - 16) / 4 {
		brands = append(brands, exportBrands[index%len(exportBrands)]...)
	}
	_, err := file.WriteAt(brands, 8)
	return err
}
