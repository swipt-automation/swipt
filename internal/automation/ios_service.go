//go:build !android && cgo

package automation

import (
	"context"
	"errors"
	"time"

	"github.com/swipt-automation/swipt/internal/fault"
)

func (h *Host) availability() (<-chan struct{}, error) {
	return h.options.Device.phone.Availability()
}

func (h *Host) deviceLost(err error, since time.Time) bool {
	return errors.Is(err, fault.ErrUnavailable) && h.options.Device.phone.Disconnected(since)
}

func (r *runner) awaitReconnection(ctx context.Context, err error, started time.Time) (bool, error) {
	if !r.host.deviceLost(err, started) {
		return false, err
	}
	r.logger.Info("the service waits for the device: ", describeError(err))
	r.set(ServiceStarting, err)
	err = r.host.options.Device.phone.WaitConnected(ctx)
	return err == nil, err
}
