//go:build android && cgo

package graphics

/*
#include "graphics_android.h"
*/
import "C"

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"unsafe"
)

const defaultJPEGQuality = 0.85

func inspectImage(data []byte) (imageHeader, error) {
	var width, height C.int32_t
	if len(data) == 0 || !C.swipt_image_inspect(unsafe.Pointer(&data[0]), C.size_t(len(data)), &width, &height) {
		return imageHeader{}, errNotImage
	}
	return imageHeader{width: int(width), height: int(height), density: densityOf(data)}, nil
}

func decodePixels(ctx context.Context, data []byte, header imageHeader) (*raster, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := newRaster(header.width, header.height, 4)
	if message := C.swipt_image_decode(unsafe.Pointer(&data[0]), C.size_t(len(data)), (*C.uint8_t)(&out.pixels[0]), C.int32_t(out.width), C.int32_t(out.height), C.size_t(out.stride)); message != nil {
		return nil, failed(nameData, message)
	}
	return out, nil
}

func (r *raster) standard() image.Image {
	bounds := image.Rect(0, 0, r.width, r.height)
	if r.gray() {
		return &image.Gray{Pix: r.pixels, Stride: r.stride, Rect: bounds}
	}
	out := image.NewNRGBA(bounds)
	for y := range r.height {
		line, target := r.row(y), out.Pix[y*out.Stride:]
		for x := 0; x < len(line); x += 4 {
			target[x], target[x+1], target[x+2], target[x+3] = line[x+red], line[x+green], line[x+blue], line[x+alpha]
		}
	}
	return out
}

func encodePixels(r *raster, format string, quality *float64, scale float64) ([]byte, error) {
	var buffer bytes.Buffer
	switch format {
	case "png":
		encoder := png.Encoder{CompressionLevel: png.BestSpeed}
		if err := encoder.Encode(&buffer, r.standard()); err != nil {
			return nil, err
		}
		return withPNGDensity(buffer.Bytes(), scale), nil
	case "jpeg":
		chosen := defaultJPEGQuality
		if quality != nil {
			chosen = *quality
		}
		if err := jpeg.Encode(&buffer, r.standard(), &jpeg.Options{Quality: min(max(int(math.Round(chosen*100)), 1), 100)}); err != nil {
			return nil, err
		}
		return withJPEGDensity(buffer.Bytes(), scale), nil
	}
	return nil, failure(nameNotSupported, "graphics: encoding ", format, " is not available on this platform")
}

func withJPEGDensity(data []byte, scale float64) []byte {
	if len(data) < 2 {
		return data
	}
	density := uint16(min(max(math.Round(pointsPerInch*scale), 1), math.MaxUint16))
	application := []byte{0xff, 0xe0, 0, 16, 'J', 'F', 'I', 'F', 0, 1, 1, 1, 0, 0, 0, 0, 0, 0}
	binary.BigEndian.PutUint16(application[12:], density)
	binary.BigEndian.PutUint16(application[14:], density)
	out := make([]byte, 0, len(data)+len(application))
	out = append(out, data[:2]...)
	out = append(out, application...)
	return append(out, data[2:]...)
}
