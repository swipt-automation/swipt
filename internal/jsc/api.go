//go:build cgo

package jsc

/*
#include "jsc_thread.h"

extern void jsc_api_functions(void **lock, void **unlock, void **report_cost);
*/
import "C"

import "unsafe"

func APIFunctions() (lock, unlock, reportExtraMemoryCost unsafe.Pointer) {
	C.jsc_api_functions(&lock, &unlock, &reportExtraMemoryCost)
	return
}
