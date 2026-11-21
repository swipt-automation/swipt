//go:build cgo && (darwin || android)

package graphics

import (
	"context"
)

type nativeFrame interface {
	size() (width, height int)
	raster(width, height int) (*raster, error)
	detach()
	release()
}

type frameSource struct {
	native        nativeFrame
	width, height int
	turns         int
}

func (s *frameSource) render(context.Context) (*raster, error) {
	width, height := s.native.size()
	if width < s.width || height < s.height {
		return nil, failure(nameData, "graphics: the video frame is ", width, "x", height, " pixels, smaller than the screen's ", s.width, "x", s.height)
	}
	shown, err := s.native.raster(s.width, s.height)
	if err != nil {
		return nil, err
	}
	return turned(shown, s.turns), nil
}

func (s *frameSource) release() {
	s.native.release()
}
