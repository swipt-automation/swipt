//go:build darwin && cgo

package graphics

/*
#include <stdlib.h>
#include "graphics.h"
*/
import "C"

import (
	"encoding/json"
	"runtime/cgo"
	"unsafe"

	"github.com/swipt-automation/swipt/internal/automation"
)

type scope struct {
	call    *automation.GraphicsCall
	publish func(any)
}

func scopeOf(handle C.uintptr_t) *scope {
	return cgo.Handle(handle).Value().(*scope)
}

//export swiptGraphicsRelease
func swiptGraphicsRelease(handle C.uintptr_t) {
	cgo.Handle(handle).Delete()
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

//export swiptGraphicsDirectory
func swiptGraphicsDirectory(handle C.uintptr_t, root, path *C.char, create C.bool) C.swipt_graphics_directory {
	directory, err := scopeOf(handle).call.Directory(C.GoString(root), C.GoString(path), bool(create))
	if err != nil {
		return C.swipt_graphics_directory{failure: C.CString(automation.FailureOf(err))}
	}
	return C.swipt_graphics_directory{path: C.CString(directory)}
}

//export swiptGraphicsStageFile
func swiptGraphicsStageFile(handle C.uintptr_t, root, path *C.char) C.swipt_graphics_staged_file {
	staged, err := scopeOf(handle).call.StageFile(C.GoString(root), C.GoString(path))
	if err != nil {
		return C.swipt_graphics_staged_file{failure: C.CString(automation.FailureOf(err))}
	}
	return C.swipt_graphics_staged_file{file: C.uintptr_t(cgo.NewHandle(staged)), path: C.CString(staged.Path())}
}

//export swiptGraphicsCommitFile
func swiptGraphicsCommitFile(file C.uintptr_t) *C.char {
	if err := cgo.Handle(file).Value().(*automation.StagedFile).Commit(); err != nil {
		return C.CString(automation.FailureOf(err))
	}
	return nil
}

//export swiptGraphicsDiscardFile
func swiptGraphicsDiscardFile(file C.uintptr_t) {
	cgo.Handle(file).Value().(*automation.StagedFile).Discard()
}

//export swiptGraphicsScreenshot
func swiptGraphicsScreenshot(handle C.uintptr_t) C.swipt_graphics_screenshot {
	data, scale, err := scopeOf(handle).call.Screenshot()
	if err != nil {
		return C.swipt_graphics_screenshot{failure: C.CString(automation.FailureOf(err))}
	}
	return C.swipt_graphics_screenshot{image: C.CBytes(data), length: C.size_t(len(data)), scale: C.double(scale)}
}

//export swiptGraphicsPublish
func swiptGraphicsPublish(handle C.uintptr_t, value *C.char) {
	scopeOf(handle).publish(json.RawMessage(C.GoString(value)))
}

//export swiptGraphicsLogError
func swiptGraphicsLogError(message *C.char) {
	current.Load().options.Logger.Error(C.GoString(message))
}

//export swiptGraphicsRecordingFailed
func swiptGraphicsRecordingFailed(message *C.char) {
	options := current.Load().options
	text := C.GoString(message)
	options.Logger.Error("save the abandoned recording: ", text)
	if options.OnRecordingFailed != nil {
		options.OnRecordingFailed(text)
	}
}
