//go:build darwin && cgo

package dnssd

/*
#include <stdlib.h>
#include "dnssd.h"
*/
import "C"

import (
	"context"
	"errors"
	"runtime/cgo"
	"strconv"
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/sys/unix"
)

const pollInterval = 200

type Error int32

func (e Error) Error() string {
	return "dnssd: error " + strconv.Itoa(int(e))
}

func check(code C.DNSServiceErrorType) error {
	if code == C.kDNSServiceErr_NoError {
		return nil
	}
	return Error(code)
}

type Advertisement struct {
	ref        C.DNSServiceRef
	handle     cgo.Handle
	registered chan error
	stop       chan struct{}
	done       chan struct{}
	failed     error
}

func (a *Advertisement) pump() {
	defer close(a.done)
	fd := int32(C.DNSServiceRefSockFD(a.ref))
	for {
		select {
		case <-a.stop:
			return
		default:
		}
		ready, err := unix.Poll([]unix.PollFd{{Fd: fd, Events: unix.POLLIN}}, pollInterval)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			a.failed = E.Cause(err, "dnssd: poll")
			return
		}
		if ready == 0 {
			continue
		}
		if err = check(C.DNSServiceProcessResult(a.ref)); err != nil {
			a.failed = err
			return
		}
	}
}

//export swiptDNSSDRegistered
func swiptDNSSDRegistered(ref C.DNSServiceRef, flags C.DNSServiceFlags, code C.DNSServiceErrorType, name *C.char, serviceType *C.char, domain *C.char, context unsafe.Pointer) {
	select {
	case cgo.Handle(uintptr(context)).Value().(*Advertisement).registered <- check(code):
	default:
	}
}

func encodeTXT(txt map[string]string) []byte {
	var record []byte
	for key, value := range txt {
		item := key + "=" + value
		record = append(append(record, byte(len(item))), item...)
	}
	return record
}

func Advertise(ctx context.Context, name string, serviceType string, port uint16, txt map[string]string) (*Advertisement, error) {
	a := &Advertisement{registered: make(chan error, 1), stop: make(chan struct{}), done: make(chan struct{})}
	if err := check(C.DNSServiceCreateConnection(&a.ref)); err != nil {
		return nil, E.Cause(err, "dnssd: connect")
	}
	a.handle = cgo.NewHandle(a)
	cName, cType := C.CString(name), C.CString(serviceType)
	defer C.free(unsafe.Pointer(cName))
	defer C.free(unsafe.Pointer(cType))
	record := encodeTXT(txt)
	var recordPointer unsafe.Pointer
	if len(record) > 0 {
		recordPointer = unsafe.Pointer(&record[0])
	}
	var ref C.DNSServiceRef
	err := check(C.swipt_dnssd_register(a.ref, &ref, cName, cType, C.uint16_t(port), C.uint16_t(len(record)), recordPointer, C.uintptr_t(a.handle)))
	go a.pump()
	if err == nil {
		select {
		case err = <-a.registered:
		case <-a.done:
			err = a.failed
		case <-ctx.Done():
			a.Close()
			return nil, ctx.Err()
		}
	}
	if err != nil {
		a.Close()
		return nil, E.Cause(err, "dnssd: register ", serviceType)
	}
	return a, nil
}

func (a *Advertisement) Close() {
	close(a.stop)
	<-a.done
	C.DNSServiceRefDeallocate(a.ref)
	a.handle.Delete()
}
