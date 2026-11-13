//go:build cgo

package automation

import (
	"context"
	"math"
	"syscall"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/instruments"
	"github.com/swipt-automation/swipt/internal/device"
)

const (
	defaultUsageInterval = time.Second
	lastSignal           = 31
)

type resourceUsage struct {
	Time           int64     `json:"time"`
	PageSize       int       `json:"pageSize"`
	CPULoadPerCore []float64 `json:"cpuLoadPerCore"`
	System         []any     `json:"system"`
	Processes      [][]any   `json:"processes"`
}

func watchResourceUsage(ctx context.Context, call *operation, publish func(any), ended func(error)) error {
	var options struct {
		IntervalMs        *float64 `json:"intervalMs"`
		ProcessAttributes []string `json:"processAttributes"`
		SystemAttributes  []string `json:"systemAttributes"`
	}
	if err := call.decode(0, &options); err != nil {
		return err
	}
	interval, err := duration("intervalMs", options.IntervalMs)
	if err != nil {
		return err
	}
	if options.IntervalMs == nil {
		interval = defaultUsageInterval
	}
	if interval < time.Millisecond {
		return E.Cause1(ErrInvalid, E.New("intervalMs must be at least 1"))
	}
	phone, err := call.phone()
	if err != nil {
		return err
	}
	go func() {
		ended(phone.WatchResourceUsage(ctx, interval, options.ProcessAttributes, options.SystemAttributes, func(usage device.ResourceUsage) {
			publish(resourceUsage{
				Time:           usage.Time.UnixMilli(),
				PageSize:       instruments.SysmonPageSize,
				CPULoadPerCore: usage.CoreLoads,
				System:         common.Map(usage.System, finite),
				Processes: common.Map(usage.Processes, func(it device.ProcessUsage) []any {
					return append([]any{it.PID, it.Executable, it.BundleID}, common.Map(it.Values, finite)...)
				}),
			})
		}))
	}()
	return nil
}

func finite(value any) any {
	if number, isFloat := value.(float64); isFloat && (math.IsNaN(number) || math.IsInf(number, 0)) {
		return nil
	}
	return value
}

type process struct {
	PID            uint64 `json:"pid"`
	ExecutablePath string `json:"executablePath,omitempty"`
	BundleID       string `json:"bundleId,omitempty"`
}

func processList(ctx context.Context, call *operation) (any, error) {
	if err := call.decode(0); err != nil {
		return nil, err
	}
	phone, err := call.phone()
	if err != nil {
		return nil, err
	}
	processes, err := phone.Processes(ctx)
	if err != nil {
		return nil, err
	}
	return common.Map(processes, func(it device.Process) process {
		return process{PID: it.PID, ExecutablePath: it.Executable, BundleID: it.BundleID}
	}), nil
}

func processKill(ctx context.Context, call *operation) (any, error) {
	var pid, signal *float64
	if err := call.decode(2, &pid, &signal); err != nil {
		return nil, err
	}
	if *pid < 0 || *pid != float64(uint64(*pid)) {
		return nil, E.Cause1(ErrInvalid, E.New("the process ID must be a non-negative integer"))
	}
	if *signal < 1 || *signal > lastSignal || *signal != float64(int(*signal)) {
		return nil, E.Cause1(ErrInvalid, E.New("the signal must be an integer from 1 to ", lastSignal))
	}
	phone, err := call.phone()
	if err != nil {
		return nil, err
	}
	return nil, phone.SignalProcess(ctx, uint64(*pid), syscall.Signal(*signal))
}
