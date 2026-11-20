//go:build linux

package netwatch

import (
	"context"
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

const netlinkGroups = unix.RTMGRP_LINK |
	unix.RTMGRP_IPV4_IFADDR |
	unix.RTMGRP_IPV6_IFADDR |
	unix.RTMGRP_IPV4_ROUTE |
	unix.RTMGRP_IPV6_ROUTE

func Watch(ctx context.Context) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	if err = unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Groups: netlinkGroups}); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "netlink")
	changes := make(chan struct{}, 1)
	stop := context.AfterFunc(ctx, func() {
		_ = file.Close()
	})
	go func() {
		defer close(changes)
		defer stop()
		defer file.Close()
		buffer := make([]byte, unix.Getpagesize())
		for {
			n, err := file.Read(buffer)
			if n > 0 || errors.Is(err, unix.ENOBUFS) {
				select {
				case changes <- struct{}{}:
				default:
				}
			}
			if err != nil && !errors.Is(err, unix.ENOBUFS) {
				return
			}
		}
	}()
	return changes, nil
}
