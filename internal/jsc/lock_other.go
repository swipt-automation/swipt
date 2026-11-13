//go:build cgo && !darwin

package jsc

/*
#include "jsc_thread.h"

extern void *jsc_api_lock(JSContextRef context);
extern void jsc_api_unlock(void *lock);
*/
import "C"

import "unsafe"

func LockFunctions() (lock unsafe.Pointer, unlock unsafe.Pointer) {
	return unsafe.Pointer(C.jsc_api_lock), unsafe.Pointer(C.jsc_api_unlock)
}
