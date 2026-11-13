//go:build !windows

package device

import "syscall"

var unreachableErrors = []error{
	syscall.ECONNREFUSED, syscall.ETIMEDOUT, syscall.EHOSTUNREACH,
	syscall.ENETUNREACH, syscall.ENETDOWN, syscall.EHOSTDOWN, syscall.EADDRNOTAVAIL,
}
