//go:build cgo

package graphics

import (
	"runtime/cgo"

	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/jsc"
	"github.com/swipt-automation/swipt/internal/log"
)

type Options struct {
	Connection        *device.Connection
	Logger            log.ContextLogger
	OnRecordingFailed func(message string)
}

type Graphics struct {
	connection        *device.Connection
	logger            log.ContextLogger
	onRecordingFailed func(message string)
}

func New(options Options) *Graphics {
	g := &Graphics{connection: options.Connection, logger: options.Logger, onRecordingFailed: options.OnRecordingFailed}
	setupNative(g)
	return g
}

func (g *Graphics) Install(context *jsc.Context, native jsc.Value) automation.GraphicsEngine {
	e := newEngine(g)
	context.RunNative(nativeInstaller(), 0, native, uint64(cgo.NewHandle(e)))
	return e
}
