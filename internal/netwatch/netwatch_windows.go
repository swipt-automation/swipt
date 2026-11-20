//go:build windows

package netwatch

import (
	"context"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iphlpapi = windows.NewLazySystemDLL("iphlpapi.dll")

	procNotifyRouteChange2           = iphlpapi.NewProc("NotifyRouteChange2")
	procNotifyUnicastIPAddressChange = iphlpapi.NewProc("NotifyUnicastIpAddressChange")
	procNotifyIPInterfaceChange      = iphlpapi.NewProc("NotifyIpInterfaceChange")
	procCancelMibChangeNotify2       = iphlpapi.NewProc("CancelMibChangeNotify2")

	manageAccess   sync.Mutex
	watchersAccess sync.Mutex
	watchers       = make(map[chan struct{}]struct{})
	handles        []windows.Handle
	callbackOnce   sync.Once
	callbackPtr    uintptr
)

func onNetworkChange(_, _ uintptr, _ uint32) uintptr {
	watchersAccess.Lock()
	for ch := range watchers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	watchersAccess.Unlock()
	return 0
}

func register(proc *windows.LazyProc) (windows.Handle, error) {
	if err := proc.Find(); err != nil {
		return 0, err
	}
	var handle windows.Handle
	r0, _, _ := syscall.SyscallN(proc.Addr(), uintptr(windows.AF_UNSPEC), callbackPtr, 0, 0, uintptr(unsafe.Pointer(&handle)))
	if r0 != 0 {
		return 0, syscall.Errno(r0)
	}
	return handle, nil
}

func unregisterAll(registered []windows.Handle) {
	if procCancelMibChangeNotify2.Find() != nil {
		return
	}
	for _, handle := range registered {
		_, _, _ = syscall.SyscallN(procCancelMibChangeNotify2.Addr(), uintptr(handle))
	}
}

func Watch(ctx context.Context) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	callbackOnce.Do(func() {
		callbackPtr = windows.NewCallback(onNetworkChange)
	})
	changes := make(chan struct{}, 1)
	manageAccess.Lock()
	if len(handles) == 0 {
		var started []windows.Handle
		for _, proc := range []*windows.LazyProc{procNotifyRouteChange2, procNotifyUnicastIPAddressChange, procNotifyIPInterfaceChange} {
			handle, err := register(proc)
			if err != nil {
				unregisterAll(started)
				manageAccess.Unlock()
				return nil, err
			}
			started = append(started, handle)
		}
		handles = started
	}
	watchersAccess.Lock()
	watchers[changes] = struct{}{}
	watchersAccess.Unlock()
	manageAccess.Unlock()
	context.AfterFunc(ctx, func() {
		manageAccess.Lock()
		watchersAccess.Lock()
		delete(watchers, changes)
		close(changes)
		empty := len(watchers) == 0
		watchersAccess.Unlock()
		var stopping []windows.Handle
		if empty {
			stopping, handles = handles, nil
		}
		manageAccess.Unlock()
		unregisterAll(stopping)
	})
	return changes, nil
}
