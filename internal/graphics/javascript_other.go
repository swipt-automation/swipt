//go:build cgo && !darwin && !android

package graphics

/*
#include "graphics.h"
*/
import "C"

var operationNames = map[C.int32_t]string{
	C.SWIPT_GRAPHICS_CROP:      "crop",
	C.SWIPT_GRAPHICS_RESIZE:    "resize",
	C.SWIPT_GRAPHICS_ROTATE:    "rotate",
	C.SWIPT_GRAPHICS_GRAYSCALE: "grayscale",
	C.SWIPT_GRAPHICS_THRESHOLD: "threshold",
	C.SWIPT_GRAPHICS_MASK:      "mask",
	C.SWIPT_GRAPHICS_INVERT:    "invert",
	C.SWIPT_GRAPHICS_DILATE:    "dilate",
	C.SWIPT_GRAPHICS_ERODE:     "erode",
}

//export swiptGraphicsDerive
func swiptGraphicsDerive(handle C.uintptr_t, operation C.int32_t, values *C.double, count C.size_t, out *C.swipt_graphics_failure) C.uintptr_t {
	setFailure(out, unsupported(operationNames[operation]))
	return 0
}

//export swiptGraphicsRecordingFrame
func swiptGraphicsRecordingFrame(handle C.uintptr_t, at C.double, out *C.swipt_graphics_failure) C.uintptr_t {
	setFailure(out, unsupported("frame"))
	return 0
}
