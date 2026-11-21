//go:build cgo && !darwin && !android

package graphics

import "math"

type imageNode struct {
	width     int
	height    int
	transform affine
	time      float64
	timestamp float64
	source    *encodedSource
}

func newImageNode(width, height int, transform affine, time, timestamp float64, source *encodedSource) *imageNode {
	return &imageNode{width: width, height: height, transform: transform, time: time, timestamp: timestamp, source: source}
}

func (i *imageNode) retain() *imageNode {
	return i
}

func (i *imageNode) release() {}

func (i *imageNode) scale() float64 {
	return 1 / math.Sqrt(math.Abs(i.transform.a*i.transform.d-i.transform.b*i.transform.c))
}

func (i *imageNode) frame() rect {
	return i.transform.applyRect(rect{0, 0, float64(i.width), float64(i.height)})
}

func (i *imageNode) estimatedBytes() int {
	return i.width * i.height * 4
}
