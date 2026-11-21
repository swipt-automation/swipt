//go:build cgo && (darwin || android)

package graphics

import (
	"context"
	"math"
	"sync"
)

type imageSource interface {
	render(ctx context.Context) (*raster, error)
	release()
}

type imageNode struct {
	width     int
	height    int
	transform affine
	gray      bool
	time      float64
	timestamp float64

	rendering sync.Mutex

	access     sync.Mutex
	references int
	source     imageSource
	parent     *imageNode
	operation  func(input *raster) *raster
	cached     *raster
}

var errImageReleased = failure(nameState, "graphics: the image was released")

func newImageNode(width, height int, transform affine, time, timestamp float64, source imageSource) *imageNode {
	return &imageNode{width: width, height: height, transform: transform, time: time, timestamp: timestamp, references: 1, source: source}
}

func (i *imageNode) derive(operation func(input *raster) *raster, width, height int, transform affine, gray bool) *imageNode {
	i.retain()
	return &imageNode{width: width, height: height, transform: transform, gray: gray, time: i.time, timestamp: i.timestamp, references: 1, parent: i, operation: operation}
}

func (i *imageNode) retain() *imageNode {
	i.access.Lock()
	defer i.access.Unlock()
	i.references++
	return i
}

func (i *imageNode) release() {
	i.access.Lock()
	i.references--
	if i.references > 0 {
		i.access.Unlock()
		return
	}
	source, parent := i.source, i.parent
	i.source, i.parent, i.operation, i.cached = nil, nil, nil, nil
	i.access.Unlock()
	if source != nil {
		source.release()
	}
	if parent != nil {
		parent.release()
	}
}

func (i *imageNode) scale() float64 {
	return 1 / math.Sqrt(math.Abs(i.transform.a*i.transform.d-i.transform.b*i.transform.c))
}

func (i *imageNode) frame() rect {
	return i.transform.applyRect(rect{0, 0, float64(i.width), float64(i.height)})
}

func (i *imageNode) estimatedBytes() int {
	if i.gray {
		return i.width * i.height
	}
	return i.width * i.height * 4
}

func (i *imageNode) pixelRect(space rect) (rect, error) {
	mapped, overlaps := i.transform.inverse().applyRect(space).intersection(rect{0, 0, float64(i.width), float64(i.height)})
	left := math.Floor(mapped.x + 1e-6)
	top := math.Floor(mapped.y + 1e-6)
	right := math.Ceil(mapped.maxX() - 1e-6)
	bottom := math.Ceil(mapped.maxY() - 1e-6)
	if !overlaps || right-left < 1 || bottom-top < 1 {
		return rect{}, failure(nameRange, "[", space, "] does not overlap the image [", i.frame(), "]")
	}
	return rect{left, top, right - left, bottom - top}, nil
}

func (i *imageNode) pixelPoint(space point) (int, int) {
	location := i.transform.inverse().apply(space)
	return int(math.Floor(location.x)), int(math.Floor(location.y))
}

func (i *imageNode) spaceRect(pixels rect) rect {
	return i.transform.applyRect(pixels)
}

func (i *imageNode) spacePoint(pixels point) point {
	return i.transform.apply(pixels)
}

func (i *imageNode) crop(space rect) (*imageNode, error) {
	pixels, err := i.pixelRect(space)
	if err != nil {
		return nil, err
	}
	x, y, width, height := int(pixels.x), int(pixels.y), int(pixels.width), int(pixels.height)
	return i.derive(func(input *raster) *raster {
		return input.view(x, y, width, height).clone()
	}, width, height, translation(pixels.x, pixels.y).then(i.transform), i.gray), nil
}

func (i *imageNode) resize(width, height int) *imageNode {
	return i.derive(func(input *raster) *raster {
		return resampled(input, width, height)
	}, width, height, scaling(float64(i.width)/float64(width), float64(i.height)/float64(height)).then(i.transform), i.gray)
}

func (i *imageNode) rotate(degrees float64) *imageNode {
	normalized := math.Mod(degrees, 360)
	if normalized < 0 {
		normalized += 360
	}
	if normalized == 0 {
		return i.retain()
	}
	angle := normalized * math.Pi / 180
	quarter := math.Mod(normalized, 90) == 0
	quarters := int(normalized) / 90
	var width, height int
	switch {
	case quarter && quarters%2 == 0:
		width, height = i.width, i.height
	case quarter:
		width, height = i.height, i.width
	default:
		width = int(math.Ceil(math.Abs(float64(i.width)*math.Cos(angle)) + math.Abs(float64(i.height)*math.Sin(angle)) - 1e-6))
		height = int(math.Ceil(math.Abs(float64(i.width)*math.Sin(angle)) + math.Abs(float64(i.height)*math.Cos(angle)) - 1e-6))
	}
	width, height = max(width, 1), max(height, 1)
	toParent := translation(-float64(width)/2, -float64(height)/2).then(rotation(-angle)).then(translation(float64(i.width)/2, float64(i.height)/2))
	return i.derive(func(input *raster) *raster {
		if quarter {
			return turned(input, quarters)
		}
		return rotated(input, normalized, width, height)
	}, width, height, toParent.then(i.transform), i.gray)
}

func (i *imageNode) grayscale() *imageNode {
	return i.derive((*raster).grayscale, i.width, i.height, i.transform, true)
}

func (i *imageNode) threshold(level int) *imageNode {
	return i.derive(func(input *raster) *raster {
		return input.thresholded(level)
	}, i.width, i.height, i.transform, true)
}

func (i *imageNode) mask(low, high [3]byte) *imageNode {
	return i.derive(func(input *raster) *raster {
		return input.masked(low, high)
	}, i.width, i.height, i.transform, true)
}

func (i *imageNode) invert() *imageNode {
	return i.derive((*raster).inverted, i.width, i.height, i.transform, i.gray)
}

func (i *imageNode) morph(radius int, dilate bool) *imageNode {
	return i.derive(func(input *raster) *raster {
		return input.morphed(radius, dilate)
	}, i.width, i.height, i.transform, i.gray)
}

func (i *imageNode) raster(ctx context.Context) (*raster, error) {
	i.rendering.Lock()
	defer i.rendering.Unlock()
	rendered, err := i.render(ctx)
	if err != nil {
		return nil, err
	}
	i.access.Lock()
	source, parent := i.source, i.parent
	i.cached, i.source, i.parent, i.operation = rendered, nil, nil, nil
	i.access.Unlock()
	if source != nil {
		source.release()
	}
	if parent != nil {
		parent.release()
	}
	return rendered, nil
}

func (i *imageNode) render(ctx context.Context) (*raster, error) {
	i.access.Lock()
	cached, source, parent, operation := i.cached, i.source, i.parent, i.operation
	if parent != nil {
		parent.retain()
	}
	i.access.Unlock()
	switch {
	case cached != nil:
		return cached, nil
	case parent != nil:
		defer parent.release()
		parent.rendering.Lock()
		input, err := parent.render(ctx)
		parent.rendering.Unlock()
		if err != nil {
			return nil, err
		}
		if err = ctx.Err(); err != nil {
			return nil, err
		}
		return operation(input), nil
	case source == nil:
		return nil, errImageReleased
	}
	rendered, err := source.render(ctx)
	if err != nil {
		return nil, err
	}
	if rendered.width != i.width || rendered.height != i.height || rendered.gray() != i.gray {
		return nil, failure(nameData, "graphics: the image decoded as ", rendered.width, "x", rendered.height, " pixels instead of ", i.width, "x", i.height)
	}
	i.access.Lock()
	taken := i.source == source
	if taken {
		i.cached, i.source = rendered, nil
	}
	i.access.Unlock()
	if taken {
		source.release()
	}
	return rendered, nil
}

func (i *imageNode) rasterIn(ctx context.Context, region *rect) (*raster, point, error) {
	var pixels rect
	if region != nil {
		var err error
		if pixels, err = i.pixelRect(*region); err != nil {
			return nil, point{}, err
		}
	}
	rendered, err := i.raster(ctx)
	if err != nil || region == nil {
		return rendered, point{}, err
	}
	return rendered.view(int(pixels.x), int(pixels.y), int(pixels.width), int(pixels.height)), point{pixels.x, pixels.y}, nil
}
