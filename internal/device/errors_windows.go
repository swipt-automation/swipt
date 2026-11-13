package device

import "golang.org/x/sys/windows"

var unreachableErrors = []error{
	windows.WSAECONNREFUSED, windows.WSAETIMEDOUT, windows.WSAEHOSTUNREACH,
	windows.WSAENETUNREACH, windows.WSAENETDOWN, windows.WSAEHOSTDOWN, windows.WSAEADDRNOTAVAIL,
}
