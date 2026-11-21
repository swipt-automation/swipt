//go:build cgo && (darwin || android)

package graphics

/*
#include "graphics.h"
*/
import "C"

import (
	"runtime/cgo"
	"unsafe"
)

func clampedChannel(value C.double) byte {
	return byte(min(max(int(value), 0), 255))
}

//export swiptGraphicsDerive
func swiptGraphicsDerive(handle C.uintptr_t, operation C.int32_t, values *C.double, count C.size_t, out *C.swipt_graphics_failure) C.uintptr_t {
	source := imageOf(handle)
	arguments := unsafe.Slice(values, int(count))
	argument := func(index int) C.double {
		if index < len(arguments) {
			return arguments[index]
		}
		return 0
	}
	var derived *imageNode
	switch operation {
	case C.SWIPT_GRAPHICS_CROP:
		cropped, err := source.crop(rect{float64(argument(0)), float64(argument(1)), float64(argument(2)), float64(argument(3))})
		if err != nil {
			setFailure(out, err)
			return 0
		}
		derived = cropped
	case C.SWIPT_GRAPHICS_RESIZE:
		derived = source.resize(max(int(argument(0)), 1), max(int(argument(1)), 1))
	case C.SWIPT_GRAPHICS_ROTATE:
		derived = source.rotate(float64(argument(0)))
	case C.SWIPT_GRAPHICS_GRAYSCALE:
		derived = source.grayscale()
	case C.SWIPT_GRAPHICS_THRESHOLD:
		derived = source.threshold(int(argument(0)))
	case C.SWIPT_GRAPHICS_MASK:
		derived = source.mask(
			[3]byte{clampedChannel(argument(0)), clampedChannel(argument(1)), clampedChannel(argument(2))},
			[3]byte{clampedChannel(argument(3)), clampedChannel(argument(4)), clampedChannel(argument(5))})
	case C.SWIPT_GRAPHICS_INVERT:
		derived = source.invert()
	case C.SWIPT_GRAPHICS_DILATE, C.SWIPT_GRAPHICS_ERODE:
		derived = source.morph(max(int(argument(0)), 1), operation == C.SWIPT_GRAPHICS_DILATE)
	default:
		setFailure(out, failure(nameType, "graphics: there is no image operation ", int(operation)))
		return 0
	}
	return C.uintptr_t(cgo.NewHandle(derived))
}

//export swiptGraphicsRecordingFrame
func swiptGraphicsRecordingFrame(handle C.uintptr_t, at C.double, out *C.swipt_graphics_failure) C.uintptr_t {
	return C.uintptr_t(cgo.NewHandle(cgo.Handle(handle).Value().(*screenRecording).frame(float64(at))))
}
