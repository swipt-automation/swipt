//go:build darwin && cgo

package graphics

/*
#cgo CFLAGS: -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Accelerate -framework AVFoundation -framework CoreGraphics -framework CoreMedia -framework CoreML -framework CoreText -framework CoreVideo -framework Foundation -framework ImageIO -framework JavaScriptCore -framework UniformTypeIdentifiers -framework VideoToolbox -framework Vision

#include <stdlib.h>
#include <CoreFoundation/CoreFoundation.h>
#include "graphics_darwin.h"
*/
import "C"

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"runtime/cgo"
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/rfb/media"
)

const framesAvailable = true

func setupNative(g *Graphics) {
	C.swipt_graphics_warm(C.uintptr_t(cgo.NewHandle(g.logger)))
}

func nativeInstaller() uintptr {
	return uintptr(unsafe.Pointer(C.swipt_graphics_install))
}

type nativeObject struct {
	pointer unsafe.Pointer
}

func (n nativeObject) release() {
	C.CFRelease(C.CFTypeRef(n.pointer))
}

func (n nativeObject) retain() frame {
	C.CFRetain(C.CFTypeRef(n.pointer))
	return n
}

func objectOf(object C.swipt_graphics_object) (any, reference, bool) {
	switch object.kind {
	case C.SWIPT_GRAPHICS_NATIVE:
		native := nativeObject{object.native}
		return native, native, true
	case C.SWIPT_GRAPHICS_HANDLE:
		return cgo.Handle(object.handle).Value(), nil, true
	}
	return nil, nil, false
}

func takenOf(object any) C.swipt_graphics_taken {
	switch typed := object.(type) {
	case nativeObject:
		return C.swipt_graphics_taken{kind: C.SWIPT_GRAPHICS_NATIVE, native: typed.pointer}
	case *screenRecording:
		C.CFRetain(C.CFTypeRef(typed.native))
		return C.swipt_graphics_taken{kind: C.SWIPT_GRAPHICS_RECORDING, handle: C.uintptr_t(cgo.NewHandle(object)), native: typed.native}
	}
	return C.swipt_graphics_taken{}
}

type scope struct {
	engine *engine
	call   *automation.GraphicsCall
}

func scopeOf(handle C.uintptr_t) *scope {
	return cgo.Handle(handle).Value().(*scope)
}

func decodeOutcome(outcome *C.char) (json.RawMessage, error) {
	defer C.free(unsafe.Pointer(outcome))
	var decoded struct {
		Value json.RawMessage     `json:"value"`
		Error *automation.Failure `json:"error"`
	}
	if err := json.Unmarshal([]byte(C.GoString(outcome)), &decoded); err != nil {
		return nil, E.Cause(err, "graphics: decode the outcome")
	}
	if decoded.Error != nil {
		return nil, decoded.Error
	}
	return decoded.Value, nil
}

type outcome struct {
	value json.RawMessage
	err   error
}

func (e *engine) runNative(c *call, _ callArguments, context *automation.GraphicsCall) (any, error) {
	objects := make([]unsafe.Pointer, max(len(c.objects), 1))
	for index, object := range c.objects {
		if native, isNative := object.(nativeObject); isNative {
			objects[index] = native.pointer
		}
	}
	owned := c.owned
	c.owned = nil
	method, arguments := C.CString(c.method), C.CString(string(c.arguments))
	handle := cgo.NewHandle(&scope{engine: e, call: context})
	done := make(chan outcome, 1)
	go func() {
		text := C.swipt_graphics_execute(method, arguments, &objects[0], C.size_t(len(c.objects)), C.uintptr_t(handle))
		handle.Delete()
		C.free(unsafe.Pointer(method))
		C.free(unsafe.Pointer(arguments))
		for _, object := range owned {
			object.release()
		}
		value, err := decodeOutcome(text)
		done <- outcome{value, err}
	}()
	select {
	case result := <-done:
		return result.value, result.err
	case <-context.Context().Done():
		return nil, context.Context().Err()
	}
}

type videoDecoder struct {
	pointer unsafe.Pointer
	stream  cgo.Handle
}

func newDecoder(stream *screenStream) decoder {
	handle := cgo.NewHandle(stream)
	pointer := C.swipt_graphics_decoder_create(C.uintptr_t(handle), C.int64_t(stream.info.Width), C.int64_t(stream.info.Height), C.double(stream.info.Scale))
	if pointer == nil {
		handle.Delete()
		return nil
	}
	return &videoDecoder{pointer: pointer, stream: handle}
}

func bytesOf(data []byte) C.swipt_graphics_bytes {
	if len(data) == 0 {
		return C.swipt_graphics_bytes{}
	}
	return C.swipt_graphics_bytes{bytes: unsafe.Pointer(&data[0]), length: C.size_t(len(data))}
}

func (d *videoDecoder) decode(unit *media.AccessUnit, format *hevcFormat, formatChanged bool, damaged bool) bool {
	decoded := C.swipt_graphics_unit{
		sample:    bytesOf(unit.Sample()),
		time:      C.int64_t(unit.Time),
		timescale: C.int32_t(media.VideoClockRate),
		arrival:   C.double(milliseconds(unit.Arrival)),
		turns:     C.int32_t(unit.Turns),
		keyframe:  C.bool(unit.Keyframe()),
		damaged:   C.bool(damaged),
	}
	if formatChanged {
		decoded.vps, decoded.sps, decoded.pps = bytesOf(format.vps), bytesOf(format.sps), bytesOf(format.pps)
	}
	return bool(C.swipt_graphics_decoder_decode(d.pointer, decoded))
}

func (d *videoDecoder) close() {
	C.swipt_graphics_decoder_close(d.pointer)
	d.stream.Delete()
}

type screenRecording struct {
	manifest *recordingManifest
	native   unsafe.Pointer
}

func newScreenRecording(directory string, manifest *recordingManifest) (*screenRecording, error) {
	segments := make([]C.swipt_graphics_segment, len(manifest.Segments))
	for index, segment := range manifest.Segments {
		file := C.CString(filepath.Join(directory, segment.File))
		defer C.free(unsafe.Pointer(file))
		segments[index] = C.swipt_graphics_segment{file: file, start: C.double(segment.Start), turns: C.int32_t(segment.Turns)}
	}
	info := C.CString(manifest.info())
	defer C.free(unsafe.Pointer(info))
	native := C.swipt_graphics_recording_open(C.swipt_graphics_recording{
		info:     info,
		start:    C.double(manifest.Start),
		duration: C.double(manifest.Duration),
		width:    C.int64_t(manifest.Screen.Width),
		height:   C.int64_t(manifest.Screen.Height),
		scale:    C.double(manifest.Screen.Scale),
		segments: &segments[0],
		count:    C.size_t(len(segments)),
	})
	recording := &screenRecording{manifest: manifest, native: native}
	runtime.AddCleanup(recording, func(native unsafe.Pointer) {
		C.CFRelease(C.CFTypeRef(native))
	}, native)
	return recording, nil
}

func (r *screenRecording) export(context *automation.GraphicsCall, target string, mp4 bool) error {
	path := C.CString(target)
	defer C.free(unsafe.Pointer(path))
	handle := cgo.NewHandle(&scope{call: context})
	defer handle.Delete()
	_, err := decodeOutcome(C.swipt_graphics_recording_export(r.native, path, C.bool(mp4), C.uintptr_t(handle)))
	runtime.KeepAlive(r)
	return err
}

func (s *screenStream) deliver(next frame) {
	s.access.Lock()
	if s.closed || s.released || s.failure != nil {
		s.access.Unlock()
		next.release()
		return
	}
	previous := s.latest
	s.latest = next
	s.sequence++
	close(s.changed)
	s.changed = make(chan struct{})
	s.access.Unlock()
	if previous != nil {
		previous.release()
	}
}

//export swiptGraphicsLog
func swiptGraphicsLog(logger C.uintptr_t, message *C.char) {
	cgo.Handle(logger).Value().(log.ContextLogger).Error(C.GoString(message))
}

//export swiptGraphicsScreenLog
func swiptGraphicsScreenLog(stream C.uintptr_t, message *C.char) {
	cgo.Handle(stream).Value().(*screenStream).engine.graphics.logger.Error(C.GoString(message))
}

//export swiptGraphicsScreenFrame
func swiptGraphicsScreenFrame(stream C.uintptr_t, image unsafe.Pointer) {
	cgo.Handle(stream).Value().(*screenStream).deliver(nativeObject{image})
}

//export swiptGraphicsRegister
func swiptGraphicsRegister(handle C.uintptr_t, native unsafe.Pointer) C.int64_t {
	return C.int64_t(scopeOf(handle).engine.register(nativeObject{native}))
}

//export swiptGraphicsCancelled
func swiptGraphicsCancelled(handle C.uintptr_t) C.bool {
	return C.bool(scopeOf(handle).call.Context().Err() != nil)
}

//export swiptGraphicsReadFile
func swiptGraphicsReadFile(handle C.uintptr_t, root, path *C.char) C.swipt_graphics_file {
	data, err := scopeOf(handle).call.ReadFile(C.GoString(root), C.GoString(path))
	if err != nil {
		return C.swipt_graphics_file{failure: C.CString(automation.FailureOf(err))}
	}
	return C.swipt_graphics_file{bytes: C.CBytes(data), length: C.size_t(len(data))}
}

//export swiptGraphicsWriteFile
func swiptGraphicsWriteFile(handle C.uintptr_t, root, path *C.char, bytes unsafe.Pointer, length C.size_t) *C.char {
	if err := scopeOf(handle).call.WriteFile(C.GoString(root), C.GoString(path), unsafe.Slice((*byte)(bytes), length)); err != nil {
		return C.CString(automation.FailureOf(err))
	}
	return nil
}

//export swiptGraphicsScreenshot
func swiptGraphicsScreenshot(handle C.uintptr_t) C.swipt_graphics_screenshot {
	data, scale, err := scopeOf(handle).call.Screenshot()
	if err != nil {
		return C.swipt_graphics_screenshot{failure: C.CString(automation.FailureOf(err))}
	}
	return C.swipt_graphics_screenshot{image: C.CBytes(data), length: C.size_t(len(data)), scale: C.double(scale)}
}
