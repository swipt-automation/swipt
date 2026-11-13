//go:build darwin && cgo

package graphics

/*
#include <stdlib.h>
#include "graphics.h"
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"

	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
)

type screenFrames struct {
	frames unsafe.Pointer
	clock  rtpClock
}

func bytesOf(data []byte) C.swipt_graphics_bytes {
	if len(data) == 0 {
		return C.swipt_graphics_bytes{}
	}
	return C.swipt_graphics_bytes{bytes: unsafe.Pointer(&data[0]), length: C.size_t(len(data))}
}

func newScreenObserver(info device.ScreenInfo) screenObserver {
	return &screenFrames{frames: C.swipt_graphics_frames_create(C.int64_t(info.Width), C.int64_t(info.Height), C.double(info.Scale))}
}

func (f *screenFrames) screenUnit(unit *accessUnit) bool {
	return bool(C.swipt_graphics_frames_decode(f.frames, C.swipt_graphics_unit{
		sample:   bytesOf(unit.sample()),
		vps:      bytesOf(unit.nal(nalVPS)),
		sps:      bytesOf(unit.nal(nalSPS)),
		pps:      bytesOf(unit.nal(nalPPS)),
		time:     C.int64_t(f.clock.time(unit.rtpTimestamp)),
		arrival:  C.double(unit.arrival),
		turns:    C.int32_t(unit.turns()),
		keyframe: C.bool(unit.keyframe()),
		damaged:  C.bool(unit.damaged),
	}))
}

func (f *screenFrames) screenEnded(err error) {
	failure := C.CString(automation.FailureOf(err))
	defer C.free(unsafe.Pointer(failure))
	C.swipt_graphics_frames_end(f.frames, failure)
}

func (f *screenFrames) screenClosed() {
	C.swipt_graphics_frames_close(f.frames)
}

//export swiptGraphicsNewScreenOwner
func swiptGraphicsNewScreenOwner() C.uintptr_t {
	return C.uintptr_t(cgo.NewHandle(newScreenOwner()))
}

//export swiptGraphicsCloseScreenOwner
func swiptGraphicsCloseScreenOwner(handle C.uintptr_t) {
	cgo.Handle(handle).Value().(*screenOwner).close()
}

//export swiptGraphicsOpenScreen
func swiptGraphicsOpenScreen(handle, owner C.uintptr_t, mode *C.char, maximumBitrate C.uint64_t) C.swipt_graphics_screen_stream {
	screens := cgo.Handle(owner).Value().(*screenOwner)
	options := device.ScreenStreamOptions{Mode: device.ScreenMode(C.GoString(mode)), MaximumBitrate: uint64(maximumBitrate)}
	stream, err := openScreen(scopeOf(handle).call.Context(), screens, options)
	if err != nil {
		return C.swipt_graphics_screen_stream{failure: C.CString(automation.FailureOf(err))}
	}
	frames := C.swipt_graphics_frames_retain(stream.source.observer.(*screenFrames).frames)
	if err = screens.add(stream); err != nil {
		return C.swipt_graphics_screen_stream{frames: frames, failure: C.CString(automation.FailureOf(err))}
	}
	return C.swipt_graphics_screen_stream{stream: C.uintptr_t(cgo.NewHandle(stream)), frames: frames}
}

//export swiptGraphicsScreenClosed
func swiptGraphicsScreenClosed(handle C.uintptr_t) C.bool {
	return C.bool(cgo.Handle(handle).Value().(*screenStream).isClosed())
}

//export swiptGraphicsScreenFailure
func swiptGraphicsScreenFailure(handle C.uintptr_t) *C.char {
	err := errScreenSilent
	if cgo.Handle(handle).Value().(*screenStream).isClosed() {
		err = errScreenClosed
	}
	return C.CString(automation.FailureOf(err))
}

//export swiptGraphicsCloseScreen
func swiptGraphicsCloseScreen(handle C.uintptr_t) {
	cgo.Handle(handle).Value().(*screenStream).close()
}

//export swiptGraphicsRecordScreen
func swiptGraphicsRecordScreen(handle, stream C.uintptr_t, root, path *C.char) C.swipt_graphics_recording_session {
	session, err := recordScreen(scopeOf(handle).call, cgo.Handle(stream).Value().(*screenStream), C.GoString(root), C.GoString(path))
	if err != nil {
		return C.swipt_graphics_recording_session{failure: C.CString(automation.FailureOf(err))}
	}
	return C.swipt_graphics_recording_session{session: C.uintptr_t(cgo.NewHandle(session))}
}

//export swiptGraphicsStopRecording
func swiptGraphicsStopRecording(handle C.uintptr_t) *C.char {
	if err := cgo.Handle(handle).Value().(*recordingSession).stop(); err != nil {
		return C.CString(automation.FailureOf(err))
	}
	return nil
}

//export swiptGraphicsReleaseObject
func swiptGraphicsReleaseObject(handle C.uintptr_t) {
	go release(cgo.Handle(handle).Value())
}
