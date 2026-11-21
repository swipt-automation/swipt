//go:build cgo

package graphics

import (
	"runtime/cgo"

	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/jsc"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/screen"
)

type Options struct {
	Screen            screen.Source
	Logger            log.ContextLogger
	OnRecordingFailed func(message string)
}

type Graphics struct {
	screen            screen.Source
	logger            log.ContextLogger
	onRecordingFailed func(message string)
}

func New(options Options) *Graphics {
	g := &Graphics{screen: options.Screen, logger: options.Logger, onRecordingFailed: options.OnRecordingFailed}
	warmVision(g.logger)
	return g
}

func (g *Graphics) Install(context *jsc.Context, native jsc.Value) automation.GraphicsEngine {
	e := newEngine(g)
	context.RunNative(installer(), 0, native, uint64(cgo.NewHandle(e)))
	return e
}
