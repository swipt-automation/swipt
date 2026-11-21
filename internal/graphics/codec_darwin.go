//go:build darwin && cgo

package graphics

/*
#include <stdlib.h>
#include "graphics_darwin.h"
*/
import "C"

import (
	"context"
	"unsafe"
)

func inspectImage(data []byte) (imageHeader, error) {
	if len(data) == 0 {
		return imageHeader{}, errNotImage
	}
	var header C.swipt_image_header
	if message := C.swipt_image_inspect(unsafe.Pointer(&data[0]), C.size_t(len(data)), &header); message != nil {
		C.free(unsafe.Pointer(message))
		return imageHeader{}, errNotImage
	}
	return imageHeader{width: int(header.width), height: int(header.height), density: float64(header.dpi) / 72}, nil
}

func decodePixels(ctx context.Context, data []byte, header imageHeader) (*raster, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := newRaster(header.width, header.height, 4)
	if message := C.swipt_image_decode(unsafe.Pointer(&data[0]), C.size_t(len(data)), bytesPointer(out.pixels), C.int64_t(out.width), C.int64_t(out.height), C.int64_t(out.stride)); message != nil {
		return nil, interrupted(ctx, failed(nameData, message))
	}
	return out, nil
}

func encodePixels(r *raster, format string, quality *float64, scale float64) ([]byte, error) {
	name := C.CString(format)
	defer C.free(unsafe.Pointer(name))
	chosen := -1.0
	if quality != nil {
		chosen = *quality
	}
	var encoded C.swipt_encoded
	if message := C.swipt_image_encode(bytesPointer(r.pixels), C.int64_t(r.width), C.int64_t(r.height), C.int64_t(r.stride), C.int32_t(r.channels), name, C.double(chosen), C.double(scale), &encoded); message != nil {
		return nil, failed(nameGeneric, message)
	}
	defer C.swipt_release(encoded.data)
	return C.GoBytes(encoded.bytes, C.int(encoded.length)), nil
}
