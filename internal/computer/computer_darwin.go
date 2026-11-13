//go:build cgo

package computer

/*
#cgo LDFLAGS: -framework CoreFoundation -framework SystemConfiguration
#include <CoreFoundation/CoreFoundation.h>
#include <SystemConfiguration/SystemConfiguration.h>
*/
import "C"

import (
	"context"
	"unsafe"

	"github.com/swipt-automation/swipt/internal/api/discovery"
	"github.com/swipt-automation/swipt/internal/dnssd"
)

var systemDiskImages = []string{"/Library/Developer/DeveloperDiskImages/iOS_DDI/Restore"}

func Name() string {
	name := C.SCDynamicStoreCopyComputerName(0, nil)
	if name == 0 {
		return ""
	}
	defer C.CFRelease(C.CFTypeRef(name))
	return goString(name)
}

func localHostName() string {
	name := C.SCDynamicStoreCopyLocalHostName(0)
	if name == 0 {
		return ""
	}
	defer C.CFRelease(C.CFTypeRef(name))
	return goString(name)
}

func goString(value C.CFStringRef) string {
	length := C.CFStringGetMaximumSizeForEncoding(C.CFStringGetLength(value), C.kCFStringEncodingUTF8) + 1
	buffer := make([]byte, length)
	if C.CFStringGetCString(value, (*C.char)(unsafe.Pointer(&buffer[0])), length, C.kCFStringEncodingUTF8) == 0 {
		return ""
	}
	return C.GoString((*C.char)(unsafe.Pointer(&buffer[0])))
}

func advertise(ctx context.Context, name string, port uint16, txt map[string]string) (string, func(), error) {
	advertisement, err := dnssd.Advertise(ctx, name, discovery.ServiceType, port, txt)
	if err != nil {
		return "", nil, err
	}
	return localHostName() + ".local", advertisement.Close, nil
}
