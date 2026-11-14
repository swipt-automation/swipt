//go:build cgo && !darwin

package graphics

/*
#cgo windows CFLAGS: -DJS_NO_EXPORT

#include <stdlib.h>
#include <string.h>
#include "native_other.h"
*/
import "C"

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"runtime/cgo"
	"strconv"
	"sync"
	"time"
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/jsc"
)

const framesAvailable = false

var setupOnce sync.Once

func setupNative(*Graphics) {
	setupOnce.Do(func() {
		lock, unlock := jsc.LockFunctions()
		C.swipt_graphics_setup(lock, unlock)
	})
}

func nativeInstaller() uintptr {
	return uintptr(unsafe.Pointer(C.swipt_graphics_install))
}

func newDecoder(*screenStream) decoder {
	return nil
}

var unsupportedCalls = map[string]string{
	"pixel":      "pixel",
	"stats":      "stats",
	"find":       "find",
	"colors":     "matchColors",
	"components": "components",
	"text":       "recognizeText",
	"diff":       "diff",
	"distance":   "distance",
	"nearest":    "nearest",
	"barcodes":   "detectBarcodes",
}

func (e *engine) runNative(c *call, arguments callArguments, context *automation.GraphicsCall) (any, error) {
	switch c.method {
	case "screenshot":
		started := milliseconds(time.Now())
		data, scale, err := context.Screenshot()
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			return nil, failure(nameUnavailable, "graphics: the device sent no screenshot")
		}
		if err = context.Context().Err(); err != nil {
			return nil, err
		}
		var given *float64
		if scale > 0 {
			given = &scale
		}
		decoded, err := decodeImage(data, given, started)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"image": e.register(decoded)}, nil
	case "decode":
		data, scale, err := c.source(context, arguments)
		if err != nil {
			return nil, err
		}
		decoded, err := decodeImage(data, scale, math.NaN())
		if err != nil {
			return nil, err
		}
		return map[string]int64{"image": e.register(decoded)}, nil
	case "encode":
		data, err := c.encoded(arguments)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"bytes": e.register(data)}, nil
	case "save":
		data, err := c.encoded(arguments)
		if err != nil {
			return nil, err
		}
		return struct{}{}, context.WriteFile(arguments.Root, arguments.Path, data)
	}
	if name, known := unsupportedCalls[c.method]; known {
		return nil, unsupported(name)
	}
	return nil, failure(nameType, "graphics: there is no call ", c.method)
}

func (c *call) image(index int) (*image, error) {
	if index < len(c.objects) {
		if found, isImage := c.objects[index].(*image); isImage {
			return found, nil
		}
	}
	return nil, failure(nameType, "graphics: argument ", index+1, " of ", c.method, " is not an Image")
}

var scaleSuffix = regexp.MustCompile(`@(\d+(?:\.\d+)?)x\.[^.]+$`)

func (c *call) source(context *automation.GraphicsCall, arguments callArguments) ([]byte, *float64, error) {
	if data, isBytes := c.first().([]byte); isBytes {
		return data, arguments.Scale, nil
	}
	if arguments.Path == "" {
		return nil, nil, failure(nameType, "decode needs bytes or a file")
	}
	data, err := context.ReadFile(arguments.Root, arguments.Path)
	if err != nil {
		return nil, nil, err
	}
	scale := arguments.Scale
	if match := scaleSuffix.FindStringSubmatch(filepath.Base(arguments.Path)); scale == nil && match != nil {
		parsed, _ := strconv.ParseFloat(match[1], 64)
		scale = &parsed
	}
	return data, scale, nil
}

func (c *call) encoded(arguments callArguments) ([]byte, error) {
	source, err := c.image(0)
	if err != nil {
		return nil, err
	}
	if arguments.Format != "" && arguments.Format != "png" {
		return nil, failure(nameNotSupported, "graphics: encoding ", arguments.Format, " is not available on this platform")
	}
	return source.encoded(), nil
}

type image struct {
	data      []byte
	width     int
	height    int
	scale     float64
	timestamp float64
}

var (
	pngSignature   = []byte("\x89PNG\r\n\x1a\n")
	otherSignature = map[string]string{"\xff\xd8\xff": "jpeg", "GIF8": "gif", "BM": "bmp", "II*\x00": "tiff", "MM\x00*": "tiff"}
)

const (
	metersPerInch = 0.0254
	pointsPerInch = 72
	pngChunkPHYs  = "pHYs"
)

func pngChunks(data []byte, visit func(kind string, chunk []byte) bool) bool {
	for at := len(pngSignature); at+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[at:]))
		end := at + 12 + length
		if length < 0 || end > len(data) || !visit(string(data[at+4:at+8]), data[at:end]) {
			return end <= len(data)
		}
		at = end
	}
	return true
}

func decodeImage(data []byte, given *float64, timestamp float64) (*image, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		for prefix, format := range otherSignature {
			if bytes.HasPrefix(data, []byte(prefix)) {
				return nil, failure(nameNotSupported, "graphics: decoding ", format, " is not available on this platform")
			}
		}
		if len(data) >= 12 && (string(data[4:8]) == "ftyp" || string(data[8:12]) == "WEBP") {
			return nil, failure(nameNotSupported, "graphics: decoding this format is not available on this platform")
		}
		return nil, failure(nameData, "graphics: the bytes are not an image")
	}
	decoded := &image{data: data, scale: 1, timestamp: timestamp}
	var density float64
	pngChunks(data, func(kind string, chunk []byte) bool {
		switch {
		case kind == "IHDR" && len(chunk) >= 20:
			decoded.width = int(binary.BigEndian.Uint32(chunk[8:]))
			decoded.height = int(binary.BigEndian.Uint32(chunk[12:]))
		case kind == pngChunkPHYs && len(chunk) >= 21 && chunk[16] == 1:
			// ImageIO rounds the resolution of a PNG to whole dots per inch
			// (sips -g dpiWidth, macOS 27.2.1 26B131).
			density = math.Round(float64(binary.BigEndian.Uint32(chunk[8:]))*metersPerInch) / pointsPerInch
		}
		return kind != "IDAT"
	})
	if decoded.width <= 0 || decoded.height <= 0 {
		return nil, failure(nameData, "graphics: the bytes are not an image")
	}
	switch {
	case given != nil:
		decoded.scale = *given
	case density >= 1 && density <= 16:
		decoded.scale = density
	}
	return decoded, nil
}

func (i *image) encoded() []byte {
	density := uint32(math.Round(pointsPerInch * i.scale / metersPerInch))
	physical := make([]byte, 21)
	binary.BigEndian.PutUint32(physical, 9)
	copy(physical[4:], pngChunkPHYs)
	binary.BigEndian.PutUint32(physical[8:], density)
	binary.BigEndian.PutUint32(physical[12:], density)
	physical[16] = 1
	binary.BigEndian.PutUint32(physical[17:], crc32.ChecksumIEEE(physical[4:17]))
	out := append(make([]byte, 0, len(i.data)+len(physical)), pngSignature...)
	complete := pngChunks(i.data, func(kind string, chunk []byte) bool {
		if kind != pngChunkPHYs {
			out = append(out, chunk...)
		}
		if kind == "IHDR" {
			out = append(out, physical...)
		}
		return true
	})
	if !complete {
		return i.data
	}
	return out
}

type screenRecording struct {
	directory string
	manifest  *recordingManifest
}

func newScreenRecording(directory string, manifest *recordingManifest) (*screenRecording, error) {
	return &screenRecording{directory: directory, manifest: manifest}, nil
}

func (r *screenRecording) export(_ *automation.GraphicsCall, target string, mp4 bool) error {
	if len(r.manifest.Segments) > 1 {
		return failure(nameNotSupported, "graphics: exporting a recording whose screen turned is not available on this platform")
	}
	file := r.manifest.Segments[0].File
	source, err := os.Open(filepath.Join(r.directory, file))
	if err != nil {
		if pathError, isPath := errors.AsType[*fs.PathError](err); isPath {
			err = pathError.Err
		}
		return failure(nameData, "graphics: read ", file, ": ", err)
	}
	defer source.Close()
	output, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	_, err = io.Copy(output, source)
	if err == nil && mp4 {
		err = rebrandMP4(output)
	}
	return E.Errors(err, output.Close())
}

func objectOf(object C.swipt_graphics_object) (any, reference, bool) {
	switch object.kind {
	case C.SWIPT_GRAPHICS_BYTES:
		return bytes.Clone(unsafe.Slice((*byte)(object.bytes), int(object.length))), nil, true
	case C.SWIPT_GRAPHICS_HANDLE:
		return cgo.Handle(object.handle).Value(), nil, true
	}
	return nil, nil, false
}

func takenOf(object any) C.swipt_graphics_taken {
	switch object.(type) {
	case *image:
		return C.swipt_graphics_taken{kind: C.SWIPT_GRAPHICS_IMAGE, handle: C.uintptr_t(cgo.NewHandle(object))}
	case *screenRecording:
		return C.swipt_graphics_taken{kind: C.SWIPT_GRAPHICS_RECORDING, handle: C.uintptr_t(cgo.NewHandle(object))}
	}
	return C.swipt_graphics_taken{}
}

//export swiptGraphicsImage
func swiptGraphicsImage(handle C.uintptr_t) C.swipt_graphics_image {
	shown := cgo.Handle(handle).Value().(*image)
	return C.swipt_graphics_image{
		width:        C.double(rounded(float64(shown.width) / shown.scale)),
		height:       C.double(rounded(float64(shown.height) / shown.scale)),
		scale:        C.double(rounded(shown.scale)),
		time:         C.double(math.NaN()),
		timestamp:    C.double(shown.timestamp),
		pixel_width:  C.int64_t(shown.width),
		pixel_height: C.int64_t(shown.height),
		cost:         C.size_t(len(shown.data)),
	}
}

//export swiptGraphicsRecordingInfo
func swiptGraphicsRecordingInfo(handle C.uintptr_t) *C.char {
	return C.CString(cgo.Handle(handle).Value().(*screenRecording).manifest.info())
}

func copyOut(data []byte, length *C.size_t) unsafe.Pointer {
	out := C.malloc(C.size_t(max(len(data), 1)))
	if len(data) > 0 {
		C.memcpy(out, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	}
	*length = C.size_t(len(data))
	return out
}

//export swiptGraphicsBytes
func swiptGraphicsBytes(handle C.uintptr_t, identifier C.int64_t, length *C.size_t) unsafe.Pointer {
	data, isBytes := engineOf(handle).take(int64(identifier)).([]byte)
	if !isBytes {
		return nil
	}
	return copyOut(data, length)
}

//export swiptGraphicsShow
func swiptGraphicsShow(handle C.uintptr_t, length *C.size_t) unsafe.Pointer {
	shown, isImage := cgo.Handle(handle).Value().(*image)
	if !isImage {
		return nil
	}
	return copyOut(shown.data, length)
}
