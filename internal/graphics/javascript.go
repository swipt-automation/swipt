//go:build cgo

package graphics

/*
#cgo windows CFLAGS: -DJS_NO_EXPORT

#include <stdlib.h>
#include <string.h>
#include "graphics.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"runtime/cgo"
	"sync"
	"unsafe"

	"github.com/swipt-automation/swipt/internal/jsc"
)

var setupJavaScript sync.Once

func installer() uintptr {
	setupJavaScript.Do(func() {
		C.swipt_graphics_setup(jsc.APIFunctions())
	})
	return uintptr(unsafe.Pointer(C.swipt_graphics_install))
}

func failed(name string, message *C.char) error {
	defer C.free(unsafe.Pointer(message))
	return failure(name, C.GoString(message))
}

func copyOut(data []byte, length *C.size_t) unsafe.Pointer {
	out := C.malloc(C.size_t(max(len(data), 1)))
	if len(data) > 0 {
		C.memcpy(out, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	}
	*length = C.size_t(len(data))
	return out
}

func imageOf(handle C.uintptr_t) *imageNode {
	return cgo.Handle(handle).Value().(*imageNode)
}

//export swiptGraphicsImage
func swiptGraphicsImage(handle C.uintptr_t) C.swipt_graphics_image {
	shown := imageOf(handle)
	frame := shown.frame()
	return C.swipt_graphics_image{
		x:            C.double(rounded(frame.x)),
		y:            C.double(rounded(frame.y)),
		width:        C.double(rounded(frame.width)),
		height:       C.double(rounded(frame.height)),
		scale:        C.double(rounded(shown.scale())),
		time:         C.double(shown.time),
		timestamp:    C.double(shown.timestamp),
		pixel_width:  C.int64_t(shown.width),
		pixel_height: C.int64_t(shown.height),
		cost:         C.size_t(shown.estimatedBytes()),
	}
}

//export swiptGraphicsShow
func swiptGraphicsShow(handle C.uintptr_t, length *C.size_t, out *C.swipt_graphics_failure) unsafe.Pointer {
	data, err := encodeImage(context.Background(), imageOf(handle), "png", nil)
	if err != nil {
		setFailure(out, err)
		return nil
	}
	return copyOut(data, length)
}

//export swiptGraphicsBytes
func swiptGraphicsBytes(handle C.uintptr_t, identifier C.int64_t, length *C.size_t) unsafe.Pointer {
	data, isBytes := engineOf(handle).take(int64(identifier)).([]byte)
	if !isBytes {
		return nil
	}
	return copyOut(data, length)
}

//export swiptGraphicsRecordingInfo
func swiptGraphicsRecordingInfo(handle C.uintptr_t) *C.char {
	return C.CString(cgo.Handle(handle).Value().(*screenRecording).manifest.info())
}

//export swiptGraphicsTextBox
func swiptGraphicsTextBox(handle C.uintptr_t, values *C.double, out *C.swipt_graphics_failure) *C.char {
	text := cgo.Handle(handle).Value().(*recognizedText)
	arguments := unsafe.Slice(values, 4)
	corners, found, err := text.native.box(int(arguments[0]), int(arguments[1]), int(arguments[2]), int(arguments[3]))
	if err != nil {
		setFailure(out, err)
		return nil
	}
	if !found {
		return C.CString("null")
	}
	encoded, err := json.Marshal(text.placement.shape(corners))
	if err != nil {
		setFailure(out, err)
		return nil
	}
	return C.CString(string(encoded))
}
