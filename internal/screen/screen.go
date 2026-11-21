package screen

import (
	"context"

	"github.com/swipt-automation/swipt/internal/rfb/media"
)

type Info struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
}

type Options struct {
	Mode           string
	MaximumBitrate uint64
	Display        int
}

type Consumer interface {
	ScreenUnit(unit *media.AccessUnit)
	ScreenMoved(info Info)
	ScreenEnded(err error)
}

type Stream interface {
	Info() Info
	RequestKeyFrame()
	Close()
}

type LocalStream interface {
	Stream
	Draw(window uintptr, drawn func(nanoseconds int64, turns int), failed func(error))
	StopDrawing(window uintptr)
	Encode(enabled bool)
	EncodedInfo() Info
}

type Source interface {
	OpenScreenStream(ctx context.Context, options Options, consumer Consumer) (Stream, error)
}
