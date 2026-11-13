//go:build cgo

package graphics

import (
	"sync"
	"sync/atomic"

	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/log"
)

type Options struct {
	Connection        *device.Connection
	Logger            log.ContextLogger
	OnRecordingFailed func(message string)
}

type Graphics struct {
	options      Options
	screenAccess sync.Mutex
	screenSource *screenSource
}

var current atomic.Pointer[Graphics]
