//go:build android && cgo

package graphics

import (
	"context"
	"math"
	"time"
)

func (c *imageCall) screenshot() (any, error) {
	started := milliseconds(time.Now())
	pixels, err := c.context.Screenshot(c.arguments.Display)
	if err != nil {
		return nil, err
	}
	shown := &raster{pixels: pixels.Data, width: pixels.Width, height: pixels.Height, stride: pixels.Stride, channels: 4}
	for y := range shown.height {
		row := shown.row(y)
		for x := 0; x < len(row); x += 4 {
			r, g, b, a := row[x], row[x+1], row[x+2], row[x+3]
			if a != 0 && a != 0xff {
				r, g, b = unpremultiplied(r, a), unpremultiplied(g, a), unpremultiplied(b, a)
			}
			row[x+red], row[x+green], row[x+blue] = r, g, b
		}
	}
	return c.registered(newImageNode(shown.width, shown.height, scaling(1, 1), math.NaN(), started, &rasterSource{shown}), nil)
}

func unpremultiplied(value, alpha byte) byte {
	return byte(min((int(value)*0xff+int(alpha)/2)/int(alpha), 0xff))
}

type rasterSource struct {
	raster *raster
}

func (s *rasterSource) render(context.Context) (*raster, error) {
	return s.raster, nil
}

func (s *rasterSource) release() {}
