//go:build cgo && (darwin || android)

package graphics

import (
	"context"
	"math"

	"github.com/swipt-automation/swipt/internal/automation"
)

const exportFrameRate = 30

type movieEncoder interface {
	append(shown *raster, milliseconds float64) error
	finish() error
	cancel()
}

func evened(side int) int {
	return max(side&^1, 2)
}

func (r *screenRecording) exportFrame(ctx context.Context, at float64) (*raster, error) {
	node := r.frame(at)
	defer node.release()
	shown, err := node.raster(ctx)
	if err != nil {
		return nil, err
	}
	return resampled(shown, evened(shown.width), evened(shown.height)), nil
}

func letterboxed(shown *raster, width, height int) *raster {
	if shown.width == width && shown.height == height {
		return shown
	}
	scale := min(float64(width)/float64(shown.width), float64(height)/float64(shown.height))
	fitted := resampled(shown, min(max(int(math.Round(float64(shown.width)*scale)), 1), width), min(max(int(math.Round(float64(shown.height)*scale)), 1), height))
	out := newRaster(width, height, shown.channels)
	fitted.copyInto(out, (width-fitted.width)/2, (height-fitted.height)/2)
	return out
}

func (r *screenRecording) export(call *automation.GraphicsCall, target string, mp4 bool) error {
	ctx := call.Context()
	first := r.frame(0)
	width, height := evened(first.width), evened(first.height)
	first.release()
	encoder, err := newMovieEncoder(ctx, target, mp4, width, height)
	if err != nil {
		return err
	}
	if err = r.encode(ctx, encoder, width, height); err != nil {
		encoder.cancel()
		return err
	}
	return encoder.finish()
}

func (r *screenRecording) encode(ctx context.Context, encoder movieEncoder, width, height int) error {
	step := 1000.0 / exportFrameRate
	for index := 0; float64(index)*step <= r.manifest.Duration; index++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		at := float64(index) * step
		shown, err := r.exportFrame(ctx, at)
		if err != nil {
			return err
		}
		if err = encoder.append(letterboxed(shown, width, height), at); err != nil {
			return err
		}
	}
	return nil
}
