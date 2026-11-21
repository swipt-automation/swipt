//go:build android && cgo

package core

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	E "github.com/sagernet/sing/common/exceptions"
	androidpb "github.com/swipt-automation/swipt/internal/android/proto"
	"github.com/swipt-automation/swipt/internal/api"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

const alertBuffer = 8

type alerts struct {
	access   sync.Mutex
	watchers map[chan *androidpb.Alert]struct{}
	pending  []*androidpb.Alert
}

func (a *alerts) subscribe() (<-chan *androidpb.Alert, func()) {
	watcher := make(chan *androidpb.Alert, alertBuffer)
	a.access.Lock()
	for _, alert := range a.pending {
		watcher <- alert
	}
	a.pending = nil
	if a.watchers == nil {
		a.watchers = make(map[chan *androidpb.Alert]struct{})
	}
	a.watchers[watcher] = struct{}{}
	a.access.Unlock()
	return watcher, func() {
		a.access.Lock()
		delete(a.watchers, watcher)
		a.access.Unlock()
	}
}

func (a *alerts) send(alert *androidpb.Alert) {
	a.access.Lock()
	defer a.access.Unlock()
	if len(a.watchers) == 0 {
		if len(a.pending) < alertBuffer {
			a.pending = append(a.pending, alert)
		}
		return
	}
	for watcher := range a.watchers {
		select {
		case watcher <- alert:
		default:
		}
	}
}

type systemService struct {
	androidpb.UnimplementedSystemServiceServer
	*Core
}

func (s *systemService) GetInfo(context.Context, *emptypb.Empty) (*androidpb.GetInfoResponse, error) {
	return &androidpb.GetInfoResponse{DeviceName: s.identity.Name}, nil
}

func (s *systemService) WatchAlerts(_ *emptypb.Empty, stream grpc.ServerStreamingServer[androidpb.Alert]) error {
	alerts, unsubscribe := s.alerts.subscribe()
	defer unsubscribe()
	for {
		select {
		case alert := <-alerts:
			if err := stream.Send(alert); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

func (s *systemService) GetCacheSize(context.Context, *emptypb.Empty) (*androidpb.GetCacheSizeResponse, error) {
	return &androidpb.GetCacheSizeResponse{Bytes: allocatedSize(s.caches)}, nil
}

func (s *systemService) ClearCaches(context.Context, *emptypb.Empty) (*emptypb.Empty, error) {
	entries, err := os.ReadDir(s.caches)
	if errors.Is(err, fs.ErrNotExist) {
		return &emptypb.Empty{}, nil
	}
	if err != nil {
		return nil, api.StatusError(err)
	}
	var errs []error
	for _, entry := range entries {
		errs = append(errs, os.RemoveAll(filepath.Join(s.caches, entry.Name())))
	}
	if err = E.Errors(errs...); err != nil {
		return nil, api.StatusError(err)
	}
	return &emptypb.Empty{}, nil
}

func allocatedSize(directory string) int64 {
	var total int64
	var walk func(string)
	walk = func(current string) {
		entries, err := os.ReadDir(current)
		if err != nil {
			return
		}
		for _, entry := range entries {
			if entry.Type()&fs.ModeSymlink != 0 {
				continue
			}
			full := filepath.Join(current, entry.Name())
			if entry.IsDir() {
				walk(full)
				continue
			}
			if info, err := entry.Info(); err == nil && info.Mode().IsRegular() {
				if stat, ok := info.Sys().(*syscall.Stat_t); ok {
					total += stat.Blocks * 512
				} else {
					total += info.Size()
				}
			}
		}
	}
	walk(directory)
	return total
}
