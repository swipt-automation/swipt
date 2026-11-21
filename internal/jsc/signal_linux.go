//go:build cgo

package jsc

// #include "jsc_thread.h"
import "C"

func init() {
	C.jsc_release_suspend_signal()
}
