//go:build darwin && cgo

package graphics

/*
#cgo CFLAGS: -fmodules -fobjc-arc
#cgo LDFLAGS: -framework Accelerate -framework AVFoundation -framework CoreFoundation -framework CoreGraphics -framework CoreMedia -framework CoreML -framework CoreVideo -framework Foundation -framework ImageIO -framework UniformTypeIdentifiers -framework VideoToolbox -framework Vision

#include <CoreFoundation/CoreFoundation.h>
#include "graphics_darwin.h"
*/
import "C"

import (
	"context"
	"unsafe"
)

func interrupted(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func watchCancel(ctx context.Context) (unsafe.Pointer, func()) {
	token := C.swipt_graphics_token_create()
	if ctx.Err() != nil {
		C.swipt_graphics_token_cancel(token)
		return token, func() {
			C.CFRelease(C.CFTypeRef(token))
		}
	}
	C.CFRetain(C.CFTypeRef(token))
	stop := context.AfterFunc(ctx, func() {
		C.swipt_graphics_token_cancel(token)
		C.CFRelease(C.CFTypeRef(token))
	})
	return token, func() {
		if stop() {
			C.CFRelease(C.CFTypeRef(token))
		}
		C.CFRelease(C.CFTypeRef(token))
	}
}
