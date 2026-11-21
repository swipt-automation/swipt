//go:build darwin && cgo

package graphics

/*
#include "accelerate_darwin.h"
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func bytesPointer(data []byte) *C.uint8_t {
	return (*C.uint8_t)(unsafe.Pointer(unsafe.SliceData(data)))
}

func floatsPointer(data []float32) *C.float {
	return (*C.float)(unsafe.Pointer(unsafe.SliceData(data)))
}

func (r *raster) arguments() (*C.uint8_t, C.long, C.long, C.long) {
	return bytesPointer(r.pixels), C.long(r.width), C.long(r.height), C.long(r.stride)
}

func require(status C.long, operation string) {
	if status != 0 {
		panic(fmt.Sprint("graphics: ", operation, ": vImage error ", int(status)))
	}
}

func resampled(r *raster, width, height int) *raster {
	if width == r.width && height == r.height {
		return r
	}
	out := newRaster(width, height, r.channels)
	in, inWidth, inHeight, inStride := r.arguments()
	require(C.swipt_scale(in, inWidth, inHeight, inStride, bytesPointer(out.pixels), C.long(width), C.long(height), C.long(out.stride), C.int(r.channels)), "resize the image")
	return out
}

func turned(r *raster, quarters int) *raster {
	quarters = (quarters%4 + 4) % 4
	if quarters == 0 {
		return r
	}
	width, height := r.width, r.height
	if quarters%2 == 1 {
		width, height = height, width
	}
	out := newRaster(width, height, r.channels)
	in, inWidth, inHeight, inStride := r.arguments()
	require(C.swipt_turn(in, inWidth, inHeight, inStride, bytesPointer(out.pixels), C.long(out.stride), C.int(r.channels), C.int(quarters)), "rotate the image")
	return out
}

func rotated(r *raster, degrees float64, width, height int) *raster {
	out := newRaster(width, height, r.channels)
	in, inWidth, inHeight, inStride := r.arguments()
	require(C.swipt_rotate(in, inWidth, inHeight, inStride, bytesPointer(out.pixels), C.long(width), C.long(height), C.long(out.stride), C.int(r.channels), C.double(degrees)), "rotate the image")
	return out
}

func mixed(r *raster, weights [4]int, divisor int) *raster {
	out := newRaster(r.width, r.height, 1)
	matrix := [4]C.int16_t{C.int16_t(weights[0]), C.int16_t(weights[1]), C.int16_t(weights[2]), C.int16_t(weights[3])}
	in, width, height, stride := r.arguments()
	require(C.swipt_mix(in, width, height, stride, bytesPointer(out.pixels), C.long(out.stride), &matrix[0], C.int32_t(divisor)), "mix the channels")
	return out
}

func mapped(r *raster, table *[256]byte) *raster {
	out := newRaster(r.width, r.height, 1)
	in, width, height, stride := r.arguments()
	require(C.swipt_map(in, width, height, stride, bytesPointer(out.pixels), C.long(out.stride), bytesPointer(table[:])), "map the image")
	return out
}

func mappedChannels(r *raster, tables *[4][256]byte) *raster {
	out := newRaster(r.width, r.height, 4)
	in, width, height, stride := r.arguments()
	require(C.swipt_map_channels(in, width, height, stride, bytesPointer(out.pixels), C.long(out.stride), (*[256]C.uint8_t)(unsafe.Pointer(tables))), "map the image")
	return out
}

func histograms(r *raster) [][256]int {
	counts := make([]uint64, 256*r.channels)
	in, width, height, stride := r.arguments()
	require(C.swipt_histograms(in, width, height, stride, C.int(r.channels), (*C.uint64_t)(unsafe.Pointer(&counts[0]))), "count the values")
	out := make([][256]int, r.channels)
	for index, count := range counts {
		out[index/256][index%256] = int(count)
	}
	return out
}

func absoluteDifference(a, b *raster) *raster {
	out := newRaster(a.width, a.height, 1)
	C.swipt_absolute_difference(bytesPointer(a.pixels), C.long(a.stride), bytesPointer(b.pixels), C.long(b.stride), C.long(a.width), C.long(a.height), bytesPointer(out.pixels), C.long(out.stride))
	return out
}

func extreme(r *raster, size int, maximum bool) *raster {
	out := newRaster(r.width, r.height, r.channels)
	in, width, height, stride := r.arguments()
	require(C.swipt_extreme(in, width, height, stride, bytesPointer(out.pixels), C.long(out.stride), C.int(r.channels), C.long(size), C.bool(maximum)), "filter the image")
	return out
}

func planeOf(gray *raster) *plane {
	out := newPlane(gray.width, gray.height)
	in, width, height, stride := gray.arguments()
	C.swipt_plane_of(in, width, height, stride, floatsPointer(out.values))
	return out
}

func (p *plane) resampled(width, height int) *plane {
	out := newPlane(width, height)
	require(C.swipt_plane_scale(floatsPointer(p.values), C.long(p.width), C.long(p.height), floatsPointer(out.values), C.long(width), C.long(height)), "scale the plane")
	return out
}

func (p *plane) squared() *plane {
	out := newPlane(p.width, p.height)
	C.swipt_plane_square(floatsPointer(p.values), floatsPointer(out.values), C.long(p.width*p.height))
	return out
}

func sum(values []float32) float32 {
	return float32(C.swipt_sum(floatsPointer(values), C.long(len(values))))
}

func dot(a, b []float32) float32 {
	return float32(C.swipt_dot(floatsPointer(a), floatsPointer(b), C.long(len(a))))
}

func centeredProduct(values, weights *plane, mean float32) *plane {
	out := newPlane(values.width, values.height)
	C.swipt_centered_product(floatsPointer(values.values), floatsPointer(weights.values), C.float(mean), floatsPointer(out.values), C.long(len(values.values)))
	return out
}

func correlated(image, kernel *plane) *plane {
	out := newPlane(image.width-kernel.width+1, image.height-kernel.height+1)
	C.swipt_correlate(floatsPointer(image.values), C.long(image.width), C.long(image.height), floatsPointer(kernel.values), C.long(kernel.width), C.long(kernel.height), floatsPointer(out.values))
	return out
}

func windowSums(level *matchLevel, origins []point) [][3]float32 {
	positions := make([]int64, 0, len(origins)*2)
	for _, origin := range origins {
		positions = append(positions, int64(origin.x), int64(origin.y))
	}
	sums := make([][3]float32, len(origins))
	k := level.kernel
	C.swipt_window_sums(floatsPointer(level.image.values), floatsPointer(level.squares.values), C.long(level.image.width), floatsPointer(k.centered.values), floatsPointer(k.weights.values),
		C.long(k.weights.width), C.long(k.weights.height), (*C.int64_t)(unsafe.Pointer(&positions[0])), C.long(len(origins)), (*C.float)(unsafe.Pointer(&sums[0])))
	return sums
}

func colorWindowSums(target *raster, x, y int, k *colorKernel) [3][3]float32 {
	var sums [3][3]float32
	C.swipt_color_window_sums(bytesPointer(target.pixels), C.long(target.stride), C.long(x), C.long(y), floatsPointer(k.centered[0].values), floatsPointer(k.centered[1].values), floatsPointer(k.centered[2].values),
		floatsPointer(k.weights.values), C.long(k.weights.width), C.long(k.weights.height), (*C.float)(unsafe.Pointer(&sums[0])))
	return sums
}
