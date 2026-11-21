//go:build android && cgo

package graphics

import "math"

func clampByte(value float64) byte {
	return byte(min(max(math.Round(value), 0), 255))
}

const lanczosLobes = 3

type lanczosTap struct {
	start   int
	weights []float32
}

func lanczos(x float64) float64 {
	x = math.Abs(x)
	if x < 1e-9 {
		return 1
	}
	if x >= lanczosLobes {
		return 0
	}
	px := math.Pi * x
	return lanczosLobes * math.Sin(px) * math.Sin(px/lanczosLobes) / (px * px)
}

func lanczosTaps(source, target int) []lanczosTap {
	scale := float64(source) / float64(target)
	spread := max(scale, 1)
	radius := lanczosLobes * spread
	taps := make([]lanczosTap, target)
	for index := range taps {
		center := (float64(index) + 0.5) * scale
		start := max(int(math.Floor(center-radius)), 0)
		end := min(int(math.Ceil(center+radius)), source)
		weights := make([]float32, end-start)
		var sum float64
		for at := start; at < end; at++ {
			weight := lanczos((float64(at) + 0.5 - center) / spread)
			weights[at-start] = float32(weight)
			sum += weight
		}
		if sum == 0 {
			taps[index] = lanczosTap{start: min(int(center), source-1), weights: []float32{1}}
			continue
		}
		for at := range weights {
			weights[at] = float32(float64(weights[at]) / sum)
		}
		taps[index] = lanczosTap{start: start, weights: weights}
	}
	return taps
}

func resample[T byte | float32](source []T, width, height, stride, channels int, target []T, targetWidth, targetHeight, targetStride int, store func(float32) T) {
	columns := lanczosTaps(width, targetWidth)
	rows := lanczosTaps(height, targetHeight)
	rowSize := targetWidth * channels
	intermediate := make([]float32, rowSize*height)
	for y := range height {
		line := source[y*stride:]
		out := intermediate[y*rowSize:]
		for x, column := range columns {
			for channel := range channels {
				var sum float32
				for index, weight := range column.weights {
					sum += weight * float32(line[(column.start+index)*channels+channel])
				}
				out[x*channels+channel] = sum
			}
		}
	}
	accumulated := make([]float32, rowSize)
	for y, row := range rows {
		clear(accumulated)
		for index, weight := range row.weights {
			line := intermediate[(row.start+index)*rowSize : (row.start+index+1)*rowSize]
			for at, value := range line {
				accumulated[at] += weight * value
			}
		}
		out := target[y*targetStride:]
		for at, value := range accumulated {
			out[at] = store(value)
		}
	}
}

func storeByte(value float32) byte {
	return clampByte(float64(value))
}

func storeFloat(value float32) float32 {
	return value
}

func resampled(r *raster, width, height int) *raster {
	if width == r.width && height == r.height {
		return r
	}
	out := newRaster(width, height, r.channels)
	resample(r.pixels, r.width, r.height, r.stride, r.channels, out.pixels, width, height, out.stride, storeByte)
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
	channels := r.channels
	for y := range r.height {
		line := r.row(y)
		for x := range r.width {
			var tx, ty int
			switch quarters {
			case 1:
				tx, ty = r.height-1-y, x
			case 2:
				tx, ty = r.width-1-x, r.height-1-y
			case 3:
				tx, ty = y, r.width-1-x
			}
			copy(out.pixels[ty*out.stride+tx*channels:ty*out.stride+tx*channels+channels], line[x*channels:x*channels+channels])
		}
	}
	return out
}

func rotated(r *raster, degrees float64, width, height int) *raster {
	out := newRaster(width, height, r.channels)
	angle := degrees * math.Pi / 180
	cosine, sine := math.Cos(angle), math.Sin(angle)
	for y := range height {
		line := out.row(y)
		dy := float64(y) + 0.5 - float64(height)/2
		for x := range width {
			dx := float64(x) + 0.5 - float64(width)/2
			sx := cosine*dx + sine*dy + float64(r.width)/2 - 0.5
			sy := -sine*dx + cosine*dy + float64(r.height)/2 - 0.5
			r.sample(sx, sy, line[x*r.channels:x*r.channels+r.channels])
		}
	}
	return out
}

func (r *raster) sample(x, y float64, target []byte) {
	left, top := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(left), y-float64(top)
	var sums [4]float64
	for _, corner := range [4]struct {
		dx, dy int
		weight float64
	}{{0, 0, (1 - fx) * (1 - fy)}, {1, 0, fx * (1 - fy)}, {0, 1, (1 - fx) * fy}, {1, 1, fx * fy}} {
		cx, cy := left+corner.dx, top+corner.dy
		if corner.weight == 0 || cx < 0 || cy < 0 || cx >= r.width || cy >= r.height {
			continue
		}
		pixel := r.pixels[cy*r.stride+cx*r.channels:]
		if r.gray() {
			sums[0] += corner.weight * float64(pixel[0])
			continue
		}
		opacity := corner.weight * float64(pixel[alpha])
		for channel := range 3 {
			sums[channel] += opacity * float64(pixel[channel])
		}
		sums[alpha] += opacity
	}
	if r.gray() {
		target[0] = clampByte(sums[0])
		return
	}
	if sums[alpha] <= 0 {
		clear(target)
		return
	}
	for channel := range 3 {
		target[channel] = clampByte(sums[channel] / sums[alpha])
	}
	target[alpha] = clampByte(sums[alpha])
}

func mixed(r *raster, weights [4]int, divisor int) *raster {
	out := newRaster(r.width, r.height, 1)
	for y := range r.height {
		line, target := r.row(y), out.row(y)
		for x := range target {
			pixel := line[x*4 : x*4+4]
			sum := weights[0]*int(pixel[0]) + weights[1]*int(pixel[1]) + weights[2]*int(pixel[2]) + weights[3]*int(pixel[3])
			target[x] = byte(min(max(sum/divisor, 0), 255))
		}
	}
	return out
}

func mapped(r *raster, table *[256]byte) *raster {
	out := newRaster(r.width, r.height, 1)
	for y := range r.height {
		target := out.row(y)
		for x, value := range r.row(y) {
			target[x] = table[value]
		}
	}
	return out
}

func mappedChannels(r *raster, tables *[4][256]byte) *raster {
	out := newRaster(r.width, r.height, 4)
	for y := range r.height {
		target := out.row(y)
		for x, value := range r.row(y) {
			target[x] = tables[x&3][value]
		}
	}
	return out
}

func histograms(r *raster) [][256]int {
	counts := make([][256]int, r.channels)
	for y := range r.height {
		for x, value := range r.row(y) {
			counts[x%r.channels][value]++
		}
	}
	return counts
}

func absoluteDifference(a, b *raster) *raster {
	out := newRaster(a.width, a.height, 1)
	for y := range a.height {
		first, second, target := a.row(y), b.row(y), out.row(y)
		for x := range target {
			target[x] = byte(max(first[x], second[x]) - min(first[x], second[x]))
		}
	}
	return out
}

func extreme(r *raster, size int, maximum bool) *raster {
	radius := size / 2
	horizontal := newRaster(r.width, r.height, r.channels)
	line := newExtremeLine(max(r.width, r.height), radius, maximum)
	for y := range r.height {
		for channel := range r.channels {
			line.run(r.pixels[y*r.stride+channel:], horizontal.pixels[y*horizontal.stride+channel:], r.width, r.channels, r.channels)
		}
	}
	out := newRaster(r.width, r.height, r.channels)
	for x := range r.width {
		for channel := range r.channels {
			offset := x*r.channels + channel
			line.run(horizontal.pixels[offset:], out.pixels[offset:], r.height, horizontal.stride, out.stride)
		}
	}
	return out
}

type extremeLine struct {
	radius  int
	maximum bool
	padded  []byte
	forward []byte
	reverse []byte
}

func newExtremeLine(length, radius int, maximum bool) *extremeLine {
	size := length + radius*2
	return &extremeLine{radius: radius, maximum: maximum, padded: make([]byte, size), forward: make([]byte, size), reverse: make([]byte, size)}
}

func (l *extremeLine) pick(a, b byte) byte {
	if l.maximum {
		return max(a, b)
	}
	return min(a, b)
}

func (l *extremeLine) run(source, target []byte, count, sourceStep, targetStep int) {
	neutral := byte(255)
	if l.maximum {
		neutral = 0
	}
	window := l.radius*2 + 1
	size := count + l.radius*2
	padded := l.padded[:size]
	for index := range padded {
		padded[index] = neutral
	}
	for index := range count {
		padded[l.radius+index] = source[index*sourceStep]
	}
	forward := l.forward[:size]
	reverse := l.reverse[:size]
	for start := 0; start < size; start += window {
		end := min(start+window, size)
		forward[start] = padded[start]
		for index := start + 1; index < end; index++ {
			forward[index] = l.pick(forward[index-1], padded[index])
		}
		reverse[end-1] = padded[end-1]
		for index := end - 2; index >= start; index-- {
			reverse[index] = l.pick(reverse[index+1], padded[index])
		}
	}
	for index := range count {
		target[index*targetStep] = l.pick(reverse[index], forward[index+window-1])
	}
}

func planeOf(gray *raster) *plane {
	out := newPlane(gray.width, gray.height)
	for y := range gray.height {
		target := out.values[y*gray.width:]
		for x, value := range gray.row(y) {
			target[x] = float32(value) / 255
		}
	}
	return out
}

func (p *plane) resampled(width, height int) *plane {
	out := newPlane(width, height)
	resample(p.values, p.width, p.height, p.width, 1, out.values, width, height, width, storeFloat)
	return out
}

func (p *plane) squared() *plane {
	out := newPlane(p.width, p.height)
	for index, value := range p.values {
		out.values[index] = value * value
	}
	return out
}

func sum(values []float32) float32 {
	var total float32
	for _, value := range values {
		total += value
	}
	return total
}

func dot(a, b []float32) float32 {
	var total float32
	for index, value := range a {
		total += value * b[index]
	}
	return total
}

func centeredProduct(values, weights *plane, mean float32) *plane {
	out := newPlane(values.width, values.height)
	for index, value := range values.values {
		out.values[index] = (value - mean) * weights.values[index]
	}
	return out
}

func correlated(image, kernel *plane) *plane {
	out := newPlane(image.width-kernel.width+1, image.height-kernel.height+1)
	for ky := range kernel.height {
		for kx := range kernel.width {
			weight := kernel.values[ky*kernel.width+kx]
			if weight == 0 {
				continue
			}
			for y := range out.height {
				source := image.values[(y+ky)*image.width+kx:][:out.width]
				target := out.values[y*out.width:][:out.width]
				for x, value := range source {
					target[x] += weight * value
				}
			}
		}
	}
	return out
}

func windowSums(level *matchLevel, origins []point) [][3]float32 {
	k := level.kernel
	width := k.weights.width
	sums := make([][3]float32, len(origins))
	for index, origin := range origins {
		x, y := int(origin.x), int(origin.y)
		var correlation, sum, square float32
		for line := range k.weights.height {
			offset := (y+line)*level.image.width + x
			image := level.image.values[offset:][:width]
			squares := level.squares.values[offset:][:width]
			centered := k.centered.values[line*width:][:width]
			weights := k.weights.values[line*width:][:width]
			for at, value := range image {
				correlation += value * centered[at]
				sum += value * weights[at]
				square += squares[at] * weights[at]
			}
		}
		sums[index] = [3]float32{correlation, sum, square}
	}
	return sums
}

func colorWindowSums(target *raster, x, y int, k *colorKernel) [3][3]float32 {
	var sums [3][3]float32
	for channel := range 3 {
		centered := k.centered[channel]
		for line := range k.weights.height {
			row := target.row(y + line)
			for index := range k.weights.width {
				value := float32(row[(x+index)*4+channel]) / 255
				weight := k.weights.values[line*k.weights.width+index]
				sums[channel][0] += value * centered.values[line*k.weights.width+index]
				sums[channel][1] += value * weight
				sums[channel][2] += value * value * weight
			}
		}
	}
	return sums
}
