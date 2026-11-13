//go:build darwin && cgo

package automation

/*
#cgo LDFLAGS: -framework CoreFoundation

#include <CoreFoundation/CoreFoundation.h>

static void append_string(CFMutableArrayRef array, const UInt8 *bytes, CFIndex length) {
	CFStringRef string = CFStringCreateWithBytes(NULL, bytes, length, kCFStringEncodingUTF8, false);
	CFArrayAppendValue(array, string);
	CFRelease(string);
}

static CFStringRef string_at(CFArrayRef array, CFIndex index) {
	return CFArrayGetValueAtIndex(array, index);
}
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/jsc"
)

func (e *engine) preferredLocalizations(arguments []jsc.Value) (any, error) {
	values, err := texts(arguments, 1)
	if err != nil {
		return nil, err
	}
	var localizations []string
	if err = json.Unmarshal([]byte(values[0]), &localizations); err != nil {
		return nil, E.Cause(err, "decode the localizations")
	}
	return preferredLocalizations(localizations, e.host.options.Languages), nil
}

func preferredLocalizations(localizations []string, preferences []string) []string {
	if len(localizations) == 0 {
		return []string{}
	}
	available := stringArray(localizations)
	defer C.CFRelease(C.CFTypeRef(available))
	var preferred C.CFArrayRef
	if len(preferences) > 0 {
		preferred = stringArray(preferences)
		defer C.CFRelease(C.CFTypeRef(preferred))
	}
	matched := C.CFBundleCopyLocalizationsForPreferences(available, preferred)
	if matched == 0 {
		return []string{}
	}
	defer C.CFRelease(C.CFTypeRef(matched))
	count := C.CFArrayGetCount(matched)
	out := make([]string, 0, count)
	for index := range count {
		out = append(out, goString(C.string_at(matched, index)))
	}
	return out
}

func stringArray(values []string) C.CFArrayRef {
	array := C.CFArrayCreateMutable(0, C.CFIndex(len(values)), &C.kCFTypeArrayCallBacks)
	for _, value := range values {
		bytes := []byte(value)
		var pointer *C.UInt8
		if len(bytes) > 0 {
			pointer = (*C.UInt8)(unsafe.Pointer(&bytes[0]))
		}
		C.append_string(array, pointer, C.CFIndex(len(bytes)))
	}
	return C.CFArrayRef(array)
}

func goString(text C.CFStringRef) string {
	length := C.CFStringGetLength(text)
	size := C.CFStringGetMaximumSizeForEncoding(length, C.kCFStringEncodingUTF8) + 1
	buffer := make([]byte, size)
	if C.CFStringGetCString(text, (*C.char)(unsafe.Pointer(&buffer[0])), size, C.kCFStringEncodingUTF8) == 0 {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}
