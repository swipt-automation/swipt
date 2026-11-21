//go:build cgo && (darwin || android)

package graphics

import "math"

const (
	blue = iota
	green
	red
	alpha
)

type raster struct {
	pixels   []byte
	width    int
	height   int
	stride   int
	channels int
}

func newRaster(width, height, channels int) *raster {
	stride := (width*channels + 63) &^ 63
	return &raster{pixels: make([]byte, max(stride*height, 1)), width: width, height: height, stride: stride, channels: channels}
}

func (r *raster) gray() bool {
	return r.channels == 1
}

func (r *raster) row(y int) []byte {
	return r.pixels[y*r.stride : y*r.stride+r.width*r.channels]
}

func (r *raster) view(x, y, width, height int) *raster {
	return &raster{pixels: r.pixels[y*r.stride+x*r.channels:], width: width, height: height, stride: r.stride, channels: r.channels}
}

func (r *raster) clone() *raster {
	out := newRaster(r.width, r.height, r.channels)
	r.copyInto(out, 0, 0)
	return out
}

func (r *raster) copyInto(target *raster, x, y int) {
	columns := min(r.width, target.width-x)
	if columns <= 0 || r.channels != target.channels {
		return
	}
	for row := range min(r.height, target.height-y) {
		copy(target.pixels[(y+row)*target.stride+x*r.channels:], r.row(row)[:columns*r.channels])
	}
}

func (r *raster) at(x, y int) [4]int {
	pixel := r.pixels[y*r.stride+x*r.channels:]
	if r.gray() {
		return [4]int{int(pixel[0]), int(pixel[0]), int(pixel[0]), 255}
	}
	return [4]int{int(pixel[red]), int(pixel[green]), int(pixel[blue]), int(pixel[alpha])}
}

func (r *raster) grayscale() *raster {
	if r.gray() {
		return r
	}
	return mixed(r, [4]int{29, 150, 77, 0}, 256)
}

func (r *raster) lookUp(table *[256]byte) *raster {
	return mapped(r.grayscale(), table)
}

func levelTable(cut int) *[256]byte {
	var table [256]byte
	for value := max(cut, 0); value < 256; value++ {
		table[value] = 255
	}
	return &table
}

func otsu(histogram *[256]int) int {
	var total, weighted float64
	for value, count := range histogram {
		total += float64(count)
		weighted += float64(value) * float64(count)
	}
	var background, backgroundSum, best float64
	level := 0
	for value, count := range histogram {
		background += float64(count)
		if background == 0 {
			continue
		}
		foreground := total - background
		if foreground == 0 {
			break
		}
		backgroundSum += float64(value) * float64(count)
		difference := backgroundSum/background - (weighted-backgroundSum)/foreground
		if between := background * foreground * difference * difference; between > best {
			best, level = between, value
		}
	}
	return level
}

func (r *raster) thresholded(level int) *raster {
	gray := r.grayscale()
	if level < 0 {
		level = otsu(&histograms(gray)[0]) + 1
	}
	return mapped(gray, levelTable(level))
}

func (r *raster) masked(low, high [3]byte) *raster {
	within := func(value int, channel int) bool {
		return value >= int(low[channel]) && value <= int(high[channel])
	}
	if r.gray() {
		var table [256]byte
		for value := range table {
			if within(value, 0) && within(value, 1) && within(value, 2) {
				table[value] = 255
			}
		}
		return mapped(r, &table)
	}
	var flags [4][256]byte
	for value := range 256 {
		for channel := range 3 {
			if within(value, channel) {
				flags[red-channel][value] = 1 << channel
			}
		}
	}
	var all [256]byte
	all[7] = 255
	return mapped(mixed(mappedChannels(r, &flags), [4]int{1, 1, 1, 0}, 1), &all)
}

func (r *raster) inverted() *raster {
	var flipped, same [256]byte
	for value := range 256 {
		flipped[value], same[value] = byte(255-value), byte(value)
	}
	if r.gray() {
		return mapped(r, &flipped)
	}
	return mappedChannels(r, &[4][256]byte{flipped, flipped, flipped, same})
}

func (r *raster) morphed(radius int, dilate bool) *raster {
	return extreme(r, radius*2+1, dilate)
}

func (r *raster) binarized() *raster {
	return r.lookUp(levelTable(128))
}

func differenceMask(first, second *raster, threshold int) (*raster, int) {
	mask := mapped(absoluteDifference(first.grayscale(), second.grayscale()), levelTable(threshold))
	return mask, histograms(mask)[0][255]
}

type rasterStatistics struct {
	Mean      []float64 `json:"mean"`
	Min       []float64 `json:"min"`
	Max       []float64 `json:"max"`
	Deviation []float64 `json:"deviation"`
}

func (r *raster) statistics() rasterStatistics {
	counts := histograms(r)
	order := []int{0}
	if !r.gray() {
		order = []int{red, green, blue, alpha}
	}
	total := float64(r.width * r.height)
	var statistics rasterStatistics
	for _, channel := range order {
		histogram := &counts[channel]
		var sum, square float64
		low, high := -1, 0
		for value, count := range histogram {
			if count == 0 {
				continue
			}
			if low < 0 {
				low = value
			}
			high = value
			sum += float64(value) * float64(count)
			square += float64(value) * float64(value) * float64(count)
		}
		mean := sum / total
		statistics.Mean = append(statistics.Mean, rounded(mean))
		statistics.Min = append(statistics.Min, float64(max(low, 0)))
		statistics.Max = append(statistics.Max, float64(high))
		statistics.Deviation = append(statistics.Deviation, rounded(math.Sqrt(max(square/total-mean*mean, 0))))
	}
	return statistics
}
