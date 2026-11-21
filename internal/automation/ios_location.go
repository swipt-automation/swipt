//go:build !android && cgo

package automation

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/atomicfile"
	"github.com/swipt-automation/swipt/internal/device"
)

const (
	locationFile       = ".simulated-location"
	locationClearLimit = 10 * time.Second
)

type locationSimulation struct {
	access  sync.Mutex
	state   sync.Mutex
	current *simulatedLocation
	changed chan struct{}
}

type simulatedLocation struct {
	Owner     string  `json:"owner"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

func (h *Host) restoreDeviceState() {
	data, err := os.ReadFile(filepath.Join(h.options.Directory, locationFile))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	var location simulatedLocation
	if err == nil {
		err = json.Unmarshal(data, &location)
	}
	if err != nil {
		h.logger.Error(E.Cause(err, "read the simulated location"))
		return
	}
	h.options.Device.location.publish(&location)
	h.options.Device.phone.RestoreLocation(location.Latitude, location.Longitude)
}

func (h *Host) storeLocation(location *simulatedLocation) error {
	path := filepath.Join(h.options.Directory, locationFile)
	if location == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	} else {
		data, err := json.Marshal(location)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(h.options.Directory, 0o755); err != nil {
			return err
		}
		if err = atomicfile.Write(path, data, 0o600); err != nil {
			return err
		}
	}
	h.options.Device.location.publish(location)
	return nil
}

func (l *locationSimulation) publish(location *simulatedLocation) {
	l.state.Lock()
	defer l.state.Unlock()
	l.current = location
	close(l.changed)
	l.changed = make(chan struct{})
}

func (l *locationSimulation) read() (any, <-chan struct{}) {
	l.state.Lock()
	defer l.state.Unlock()
	if l.current == nil {
		return nil, l.changed
	}
	return map[string]any{"latitude": l.current.Latitude, "longitude": l.current.Longitude}, l.changed
}

func (l *locationSimulation) watch(ctx context.Context, publish func(any)) {
	for {
		location, changed := l.read()
		publish(location)
		select {
		case <-changed:
		case <-ctx.Done():
			return
		}
	}
}

func (h *Host) setLocation(ctx context.Context, phone *device.Phone, owner string, latitude, longitude float64) error {
	location := h.options.Device.location
	location.access.Lock()
	defer location.access.Unlock()
	if err := phone.SetLocation(ctx, latitude, longitude); err != nil {
		return err
	}
	return h.storeLocation(&simulatedLocation{Owner: owner, Latitude: latitude, Longitude: longitude})
}

func (h *Host) clearLocation(ctx context.Context, phone *device.Phone) error {
	location := h.options.Device.location
	location.access.Lock()
	defer location.access.Unlock()
	if err := h.storeLocation(nil); err != nil {
		return err
	}
	return phone.ClearLocation(ctx)
}

func (h *Host) releaseDeviceState(id string) {
	location := h.options.Device.location
	location.access.Lock()
	defer location.access.Unlock()
	location.state.Lock()
	owned := location.current != nil && location.current.Owner == id
	location.state.Unlock()
	if !owned {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), locationClearLimit)
	defer cancel()
	err := h.storeLocation(nil)
	if err == nil {
		err = h.options.Device.phone.ClearLocation(ctx)
	}
	if err != nil {
		h.logger.Error(E.Cause(err, "clear the simulated location"))
	}
}
