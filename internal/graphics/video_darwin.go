//go:build darwin && cgo

package graphics

/*
#include <stdlib.h>
#include "graphics_darwin.h"
*/
import "C"

import (
	"context"
	"math"
	"runtime/cgo"
	"sync"
	"unsafe"

	"github.com/swipt-automation/swipt/internal/rfb/media"
)

type pixelBuffer struct {
	access  sync.Mutex
	pointer unsafe.Pointer
}

func (b *pixelBuffer) size() (int, int) {
	var width, height C.int64_t
	C.swipt_pixel_buffer_size(b.pointer, &width, &height)
	return int(width), int(height)
}

func (b *pixelBuffer) raster(width, height int) (*raster, error) {
	b.access.Lock()
	defer b.access.Unlock()
	if b.pointer == nil {
		return nil, errImageReleased
	}
	out := newRaster(width, height, 4)
	if message := C.swipt_pixel_buffer_copy(b.pointer, bytesPointer(out.pixels), C.int64_t(width), C.int64_t(height), C.int64_t(out.stride)); message != nil {
		return nil, failed(nameGeneric, message)
	}
	return out, nil
}

func (b *pixelBuffer) detach() {}

func (b *pixelBuffer) release() {
	b.access.Lock()
	defer b.access.Unlock()
	if b.pointer != nil {
		C.swipt_release(b.pointer)
		b.pointer = nil
	}
}

type videoDecoder struct {
	pointer unsafe.Pointer
	stream  cgo.Handle
}

func newLiveFrames(stream *screenStream) (liveFrames, error) {
	handle := cgo.NewHandle(stream)
	return &videoDecoder{pointer: C.swipt_decoder_create(C.uintptr_t(handle)), stream: handle}, nil
}

func bytesOf(data []byte) C.swipt_graphics_bytes {
	if len(data) == 0 {
		return C.swipt_graphics_bytes{}
	}
	return C.swipt_graphics_bytes{bytes: unsafe.Pointer(&data[0]), length: C.size_t(len(data))}
}

func (d *videoDecoder) unit(unit *media.AccessUnit, format *hevcFormat, formatChanged bool, damaged bool) bool {
	decoded := C.swipt_graphics_unit{
		sample:    bytesOf(unit.Sample()),
		time:      C.int64_t(unit.Time),
		timescale: C.int32_t(media.VideoClockRate),
		keyframe:  C.bool(unit.Keyframe()),
		damaged:   C.bool(damaged),
	}
	if formatChanged {
		decoded.vps, decoded.sps, decoded.pps = bytesOf(format.vps), bytesOf(format.sps), bytesOf(format.pps)
	}
	return bool(C.swipt_decoder_decode(d.pointer, decoded, C.double(milliseconds(unit.Arrival)), C.int32_t(unit.Turns)))
}

func (d *videoDecoder) close() {
	C.swipt_decoder_close(d.pointer)
	d.stream.Delete()
}

//export swiptGraphicsScreenLog
func swiptGraphicsScreenLog(stream C.uintptr_t, message *C.char) {
	cgo.Handle(stream).Value().(*screenStream).engine.graphics.logger.Error(C.GoString(message))
}

//export swiptGraphicsScreenFrame
func swiptGraphicsScreenFrame(stream C.uintptr_t, buffer unsafe.Pointer, arrival C.double, turns C.int32_t) {
	s := cgo.Handle(stream).Value().(*screenStream)
	s.deliver(frameNode(&pixelBuffer{pointer: buffer}, s.info, int(turns), math.NaN(), float64(arrival)))
}

type movieFile struct {
	pointer unsafe.Pointer
}

func openMovie(path string) (movieReader, error) {
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	var reader unsafe.Pointer
	if message := C.swipt_movie_open(name, &reader); message != nil {
		return nil, failed(nameData, message)
	}
	return movieFile{reader}, nil
}

func (m movieFile) frame(ctx context.Context, seconds float64) (nativeFrame, error) {
	token, done := watchCancel(ctx)
	defer done()
	var buffer unsafe.Pointer
	if message := C.swipt_movie_frame(m.pointer, C.double(seconds), token, &buffer); message != nil {
		return nil, interrupted(ctx, failed(nameGeneric, message))
	}
	return &pixelBuffer{pointer: buffer}, nil
}

func (m movieFile) close() {
	C.swipt_release(m.pointer)
}

type assetWriter struct {
	pointer unsafe.Pointer
	ctx     context.Context
	release func()
}

func newMovieEncoder(ctx context.Context, path string, mp4 bool, width, height int) (movieEncoder, error) {
	name := C.CString(path)
	defer C.free(unsafe.Pointer(name))
	token, release := watchCancel(ctx)
	var encoder unsafe.Pointer
	if message := C.swipt_encoder_create(name, C.bool(mp4), C.int64_t(width), C.int64_t(height), token, &encoder); message != nil {
		release()
		return nil, interrupted(ctx, failed(nameGeneric, message))
	}
	return &assetWriter{pointer: encoder, ctx: ctx, release: release}, nil
}

func (w *assetWriter) append(shown *raster, milliseconds float64) error {
	if message := C.swipt_encoder_append(w.pointer, bytesPointer(shown.pixels), C.int64_t(shown.stride), C.double(milliseconds)); message != nil {
		return interrupted(w.ctx, failed(nameGeneric, message))
	}
	return nil
}

func (w *assetWriter) finish() error {
	defer w.release()
	if message := C.swipt_encoder_finish(w.pointer); message != nil {
		return interrupted(w.ctx, failed(nameGeneric, message))
	}
	return nil
}

func (w *assetWriter) cancel() {
	defer w.release()
	C.swipt_encoder_cancel(w.pointer)
}
