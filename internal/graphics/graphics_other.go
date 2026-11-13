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
	"encoding/json"
	"hash/crc32"
	"maps"
	"math"
	"path/filepath"
	"regexp"
	"runtime/cgo"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	F "github.com/sagernet/sing/common/format"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/jsc"
)

const argumentsMalformed = "graphics: the arguments are not JSON"

var setupOnce sync.Once

func New(options Options) *Graphics {
	graphics := &Graphics{options: options}
	current.Store(graphics)
	return graphics
}

func (g *Graphics) Entry() uintptr {
	return uintptr(unsafe.Pointer(C.swipt_graphics_native))
}

func (g *Graphics) Run(engine uint64, identifier int64, context *automation.GraphicsCall) (json.RawMessage, error) {
	prepared := takePrepared(engine, identifier)
	if prepared == nil {
		return nil, failure(nameAbort, "the JavaScript session ended")
	}
	value, err := g.run(prepared, context)
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (g *Graphics) Watch(engine uint64, identifier int64, context *automation.GraphicsCall, publish func(any)) error {
	prepared := takePrepared(engine, identifier)
	if prepared == nil {
		return failure(nameAbort, "the JavaScript session ended")
	}
	if prepared.method != "screen.frames" {
		return failure(nameType, "graphics: there is no stream ", prepared.method)
	}
	return unsupported("frames")
}

var unsupportedCalls = map[string]string{
	"screen.frame": "frames",
	"pixel":        "pixel",
	"stats":        "stats",
	"find":         "find",
	"colors":       "matchColors",
	"components":   "components",
	"text":         "recognizeText",
	"diff":         "diff",
	"distance":     "distance",
	"nearest":      "nearest",
	"barcodes":     "detectBarcodes",
}

func (g *Graphics) run(prepared *call, context *automation.GraphicsCall) (any, error) {
	var arguments callArguments
	if err := json.Unmarshal(prepared.arguments, &arguments); err != nil {
		return nil, failure(nameType, argumentsMalformed)
	}
	switch prepared.method {
	case "screenshot":
		started := float64(time.Now().UnixNano()) / 1e6
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
		return map[string]int64{"image": prepared.engine.register(decoded)}, nil
	case "decode":
		data, scale, err := prepared.source(context, arguments)
		if err != nil {
			return nil, err
		}
		decoded, err := decodeImage(data, scale, math.NaN())
		if err != nil {
			return nil, err
		}
		return map[string]int64{"image": prepared.engine.register(decoded)}, nil
	case "encode":
		data, err := prepared.encoded(arguments)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"bytes": prepared.engine.register(data)}, nil
	case "save":
		data, err := prepared.encoded(arguments)
		if err != nil {
			return nil, err
		}
		return struct{}{}, context.WriteFile(arguments.Root, arguments.Path, data)
	case "screen.open":
		options := device.ScreenStreamOptions{Mode: device.ScreenMode(arguments.Mode), MaximumBitrate: arguments.MaximumBitrate}
		stream, err := openScreen(context.Context(), prepared.engine.screens, options)
		if err != nil {
			return nil, err
		}
		if err = prepared.engine.screens.add(stream); err != nil {
			return nil, err
		}
		return map[string]int64{"stream": prepared.engine.register(stream)}, nil
	case "screen.close":
		stream, err := prepared.stream()
		if err != nil {
			return nil, err
		}
		stream.close()
		return struct{}{}, nil
	case "screen.record":
		stream, err := prepared.stream()
		if err != nil {
			return nil, err
		}
		session, err := recordScreen(context, stream, arguments.Root, arguments.Path)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"session": prepared.engine.register(session)}, nil
	case "recording.stop":
		session, isSession := prepared.first().(*recordingSession)
		if !isSession {
			return nil, failure(nameType, "graphics: stop needs a RecordingSession")
		}
		return struct{}{}, session.stop()
	case "recording.open":
		directory, err := context.Directory(arguments.Root, arguments.Path, false)
		if err != nil {
			return nil, err
		}
		recording, err := openRecording(directory)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"recording": prepared.engine.register(recording)}, nil
	case "recording.export":
		recording, isRecording := prepared.first().(*screenRecording)
		if !isRecording {
			return nil, failure(nameType, "graphics: ", prepared.method, " needs a ScreenRecording")
		}
		extension := strings.ToLower(filepath.Ext(arguments.Path))
		if extension != ".mov" && extension != ".mp4" {
			return nil, failure(nameType, "graphics: export to a .mov or .mp4 file")
		}
		staged, err := context.StageFile(arguments.Root, arguments.Path)
		if err != nil {
			return nil, err
		}
		if err = recording.export(staged.Path(), extension == ".mp4"); err != nil {
			staged.Discard()
			return nil, err
		}
		return struct{}{}, staged.Commit()
	}
	if name, known := unsupportedCalls[prepared.method]; known {
		return nil, unsupported(name)
	}
	return nil, failure(nameType, "graphics: there is no call ", prepared.method)
}

type callArguments struct {
	Root           string   `json:"root"`
	Path           string   `json:"path"`
	Scale          *float64 `json:"scale"`
	Format         string   `json:"format"`
	Mode           string   `json:"mode"`
	MaximumBitrate uint64   `json:"maximumBitrate"`
}

type call struct {
	engine    *engine
	method    string
	arguments json.RawMessage
	objects   []any
}

func (c *call) first() any {
	if len(c.objects) == 0 {
		return nil
	}
	return c.objects[0]
}

func (c *call) image(index int) (*image, error) {
	if index < len(c.objects) {
		if found, isImage := c.objects[index].(*image); isImage {
			return found, nil
		}
	}
	return nil, failure(nameType, "graphics: argument ", index+1, " of ", c.method, " is not an Image")
}

func (c *call) stream() (*screenStream, error) {
	stream, isStream := c.first().(*screenStream)
	if !isStream {
		return nil, failure(nameType, "graphics: ", c.method, " needs a ScreenStream")
	}
	return stream, nil
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

func unsupported(name string) error {
	return failure(nameNotSupported, "graphics: ", name, " is not available on this platform")
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
			// (sips -g dpiWidth, Mac14,3, macOS 27.0.1 26A434).
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

type engine struct {
	access   sync.Mutex
	next     int64
	prepared map[int64]*call
	objects  map[int64]any
	screens  *screenOwner
	closed   bool
}

var (
	enginesAccess sync.Mutex
	engines       = make(map[uint64]*engine)
)

func engineOf(identifier uint64) *engine {
	enginesAccess.Lock()
	defer enginesAccess.Unlock()
	return engines[identifier]
}

func takePrepared(engineIdentifier uint64, identifier int64) *call {
	e := engineOf(engineIdentifier)
	if e == nil {
		return nil
	}
	e.access.Lock()
	defer e.access.Unlock()
	prepared := e.prepared[identifier]
	delete(e.prepared, identifier)
	return prepared
}

func (e *engine) register(object any) int64 {
	e.access.Lock()
	e.next++
	identifier := e.next
	accepted := !e.closed
	if accepted {
		e.objects[identifier] = object
	}
	e.access.Unlock()
	if !accepted {
		release(object)
	}
	return identifier
}

func (e *engine) take(identifier int64) any {
	e.access.Lock()
	defer e.access.Unlock()
	object := e.objects[identifier]
	delete(e.objects, identifier)
	return object
}

type screenless struct{}

func newScreenObserver(device.ScreenInfo) screenObserver {
	return screenless{}
}

func (screenless) screenUnit(*accessUnit) bool {
	return false
}

func (screenless) screenEnded(error) {}

func (screenless) screenClosed() {}

func (e *engine) close() {
	e.access.Lock()
	e.closed = true
	objects := slices.Collect(maps.Values(e.objects))
	clear(e.prepared)
	clear(e.objects)
	e.access.Unlock()
	for _, object := range objects {
		release(object)
	}
	e.screens.close()
}

//export swiptGraphicsInstall
func swiptGraphicsInstall(identifier C.uint64_t) {
	setupOnce.Do(func() {
		lock, unlock := jsc.LockFunctions()
		C.swipt_graphics_setup(lock, unlock)
	})
	enginesAccess.Lock()
	engines[uint64(identifier)] = &engine{prepared: make(map[int64]*call), objects: make(map[int64]any), screens: newScreenOwner()}
	enginesAccess.Unlock()
}

//export swiptGraphicsUninstall
func swiptGraphicsUninstall(identifier C.uint64_t) {
	enginesAccess.Lock()
	e := engines[uint64(identifier)]
	delete(engines, uint64(identifier))
	enginesAccess.Unlock()
	if e != nil {
		go e.close()
	}
}

func setFailure(out *C.swipt_graphics_failure, name string, message ...any) {
	out.name = C.CString(name)
	out.message = C.CString(F.ToString(message...))
}

//export swiptGraphicsPrepare
func swiptGraphicsPrepare(identifier C.uint64_t, method *C.char, arguments *C.char, objects *C.swipt_graphics_object, count C.size_t, out *C.swipt_graphics_failure) C.int64_t {
	e := engineOf(uint64(identifier))
	if e == nil {
		setFailure(out, nameAbort, "the JavaScript session ended")
		return 0
	}
	prepared := &call{engine: e, method: C.GoString(method), arguments: json.RawMessage(C.GoString(arguments))}
	if !json.Valid(prepared.arguments) {
		setFailure(out, nameType, argumentsMalformed)
		return 0
	}
	for index, object := range unsafe.Slice(objects, int(count)) {
		switch object.kind {
		case C.SWIPT_GRAPHICS_BYTES:
			prepared.objects = append(prepared.objects, bytes.Clone(unsafe.Slice((*byte)(object.bytes), int(object.length))))
		case C.SWIPT_GRAPHICS_HANDLE:
			prepared.objects = append(prepared.objects, cgo.Handle(object.handle).Value())
		default:
			setFailure(out, nameType, "graphics: argument ", index+1, " of ", prepared.method, " is not an Image, a ScreenStream, a RecordingSession, a ScreenRecording or a Uint8Array")
			return 0
		}
	}
	e.access.Lock()
	defer e.access.Unlock()
	e.next++
	e.prepared[e.next] = prepared
	return C.int64_t(e.next)
}

//export swiptGraphicsTake
func swiptGraphicsTake(identifier C.uint64_t, object C.int64_t) C.swipt_graphics_taken {
	var taken C.swipt_graphics_taken
	e := engineOf(uint64(identifier))
	if e == nil {
		return taken
	}
	value := e.take(int64(object))
	switch typed := value.(type) {
	case *image:
		width, height := float64(typed.width)/typed.scale, float64(typed.height)/typed.scale
		taken.kind = C.SWIPT_GRAPHICS_IMAGE
		taken.width, taken.height, taken.scale = C.double(rounded(width)), C.double(rounded(height)), C.double(rounded(typed.scale))
		taken.time, taken.timestamp = C.double(math.NaN()), C.double(typed.timestamp)
		taken.pixel_width, taken.pixel_height = C.int64_t(typed.width), C.int64_t(typed.height)
		taken.cost = C.size_t(len(typed.data))
	case *screenRecording:
		taken.kind = C.SWIPT_GRAPHICS_RECORDING
		taken.info = C.CString(typed.info())
	case *screenStream, *recordingSession:
		taken.kind = C.SWIPT_GRAPHICS_RESOURCE
	default:
		return taken
	}
	taken.handle = C.uintptr_t(cgo.NewHandle(value))
	return taken
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
func swiptGraphicsBytes(identifier C.uint64_t, object C.int64_t, length *C.size_t) unsafe.Pointer {
	e := engineOf(uint64(identifier))
	if e == nil {
		return nil
	}
	data, isBytes := e.take(int64(object)).([]byte)
	if !isBytes {
		return nil
	}
	return copyOut(data, length)
}

//export swiptGraphicsShow
func swiptGraphicsShow(handle C.uintptr_t, length *C.size_t) unsafe.Pointer {
	if handle == 0 {
		return nil
	}
	shown, isImage := cgo.Handle(handle).Value().(*image)
	if !isImage {
		return nil
	}
	return copyOut(shown.data, length)
}

//export swiptGraphicsRelease
func swiptGraphicsRelease(handle C.uintptr_t) {
	value := cgo.Handle(handle)
	object := value.Value()
	value.Delete()
	go release(object)
}
