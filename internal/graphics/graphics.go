//go:build darwin && cgo

package graphics

/*
#cgo CFLAGS: -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Accelerate -framework AVFoundation -framework CoreGraphics -framework CoreMedia -framework CoreML -framework CoreText -framework CoreVideo -framework Foundation -framework ImageIO -framework JavaScriptCore -framework UniformTypeIdentifiers -framework VideoToolbox -framework Vision

#include <stdlib.h>
#include "graphics.h"
*/
import "C"

import (
	"encoding/json"
	"runtime/cgo"
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
)

func New(options Options) *Graphics {
	graphics := &Graphics{options: options}
	current.Store(graphics)
	C.swipt_graphics_warm()
	return graphics
}

func (g *Graphics) Entry() uintptr {
	return uintptr(unsafe.Pointer(C.swipt_graphics_native))
}

func (g *Graphics) Run(engine uint64, call int64, context *automation.GraphicsCall) (json.RawMessage, error) {
	handle := cgo.NewHandle(&scope{call: context})
	return decodeOutcome(C.swipt_graphics_run(C.uint64_t(engine), C.int64_t(call), C.uintptr_t(handle)))
}

func (g *Graphics) Watch(engine uint64, call int64, context *automation.GraphicsCall, publish func(any)) error {
	handle := cgo.NewHandle(&scope{call: context, publish: publish})
	_, err := decodeOutcome(C.swipt_graphics_watch(C.uint64_t(engine), C.int64_t(call), C.uintptr_t(handle)))
	return err
}

func decodeOutcome(outcome *C.char) (json.RawMessage, error) {
	defer C.free(unsafe.Pointer(outcome))
	var decoded struct {
		Value json.RawMessage     `json:"value"`
		Error *automation.Failure `json:"error"`
	}
	if err := json.Unmarshal([]byte(C.GoString(outcome)), &decoded); err != nil {
		return nil, E.Cause(err, "graphics: decode the outcome")
	}
	if decoded.Error != nil {
		return nil, decoded.Error
	}
	return decoded.Value, nil
}
