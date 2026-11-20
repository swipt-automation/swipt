//go:build darwin

package netwatch

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func Watch(ctx context.Context) (<-chan struct{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	syscall.ForkLock.RLock()
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, 0)
	if err == nil {
		unix.CloseOnExec(fd)
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, err
	}
	if err = unix.SetNonblock(fd, true); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	file := os.NewFile(uintptr(fd), "route")
	changes := make(chan struct{}, 1)
	stop := context.AfterFunc(ctx, func() {
		_ = file.Close()
	})
	go func() {
		defer close(changes)
		defer stop()
		defer file.Close()
		buffer := make([]byte, 4096)
		for {
			n, err := file.Read(buffer)
			if errors.Is(err, unix.ENOBUFS) {
				select {
				case changes <- struct{}{}:
				default:
				}
				continue
			}
			if err != nil {
				return
			}
			relevant := false
			for data := buffer[:n]; len(data) >= 4; {
				length := int(binary.LittleEndian.Uint16(data[:2]))
				if length < 4 || len(data) < length {
					break
				}
				if data[2] == unix.RTM_VERSION {
					switch data[3] {
					case unix.RTM_ADD, unix.RTM_DELETE, unix.RTM_CHANGE, unix.RTM_IFINFO, unix.RTM_IFINFO2, unix.RTM_NEWADDR, unix.RTM_DELADDR:
						relevant = true
					}
				}
				data = data[length:]
			}
			if relevant {
				select {
				case changes <- struct{}{}:
				default:
				}
			}
		}
	}()
	return changes, nil
}
