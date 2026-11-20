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
	"math"
	"runtime/cgo"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/sys/unix"
)

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
	wake       [2]int
	done       chan struct{}
	closeOnce  sync.Once
	failed     error
}

func (a *Advertisement) pump() {
	defer close(a.done)
	fd := int32(C.DNSServiceRefSockFD(a.ref))
	wake := int32(a.wake[0])
	for {
		fds := []unix.PollFd{
			{Fd: fd, Events: unix.POLLIN},
			{Fd: wake, Events: unix.POLLIN},
		}
		_, err := unix.Poll(fds, -1)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			a.failed = E.Cause(err, "dnssd: poll")
			return
		}
		if fds[1].Revents != 0 {
			return
		}
		if fds[0].Revents == 0 {
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

func encodeTXT(txt map[string]string) ([]byte, error) {
	var record []byte
	for key, value := range txt {
		item := key + "=" + value
		if len(item) > math.MaxUint8 {
			return nil, E.New("dnssd: the TXT entry ", key, " is longer than ", math.MaxUint8, " bytes")
		}
		record = append(append(record, byte(len(item))), item...)
	}
	if len(record) > math.MaxUint16 {
		return nil, E.New("dnssd: the TXT record is longer than ", math.MaxUint16, " bytes")
	}
	return record, nil
}

func Advertise(ctx context.Context, name string, serviceType string, port uint16, txt map[string]string) (*Advertisement, error) {
	record, err := encodeTXT(txt)
	if err != nil {
		return nil, err
	}
	a := &Advertisement{registered: make(chan error, 1), done: make(chan struct{})}
	syscall.ForkLock.RLock()
	if err = unix.Pipe(a.wake[:]); err == nil {
		unix.CloseOnExec(a.wake[0])
		unix.CloseOnExec(a.wake[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, E.Cause(err, "dnssd: pipe")
	}
	if err = check(C.DNSServiceCreateConnection(&a.ref)); err != nil {
		_ = unix.Close(a.wake[0])
		_ = unix.Close(a.wake[1])
		return nil, E.Cause(err, "dnssd: connect")
	}
	a.handle = cgo.NewHandle(a)
	cName, cType := C.CString(name), C.CString(serviceType)
	defer C.free(unsafe.Pointer(cName))
	defer C.free(unsafe.Pointer(cType))
	var recordPointer unsafe.Pointer
	if len(record) > 0 {
		recordPointer = unsafe.Pointer(&record[0])
	}
	var ref C.DNSServiceRef
	err = check(C.swipt_dnssd_register(a.ref, &ref, cName, cType, C.uint16_t(port), C.uint16_t(len(record)), recordPointer, C.uintptr_t(a.handle)))
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
	a.closeOnce.Do(func() {
		_ = unix.Close(a.wake[1])
		<-a.done
		_ = unix.Close(a.wake[0])
		C.DNSServiceRefDeallocate(a.ref)
		a.handle.Delete()
	})
}
