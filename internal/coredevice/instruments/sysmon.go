package instruments

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/dtx"
)

const (
	sysmonService  = "com.apple.instruments.server.services.sysmontap"
	sysmonChannel  = 1
	SysmonPageSize = 16384
	// The tap collects a sample as it starts and then every sampleInterval,
	// and sends what it has collected every ur, first one ur after it starts
	// (iOS 27.3 24C5037i: DVTInstrumentsFoundation 0x26713efcc, 0x26713f158;
	// libsysmon 0x2cef00d98, 0x2cef0123c).
	sysmonDelivery = 100 * time.Millisecond
)

type ProcessUsage struct {
	PID    uint64
	Values []any
}

// The tap samples system and process data with separate requests and can
// deliver them in separate messages, and it drops the process rows that did
// not change since the previous sample (DVTInstrumentsFoundation 24C5037i
// 0x267176fe0, 0x267175560). A process has no cpuUsage or powerScore until it
// has a previous sample (libsysmon 24C5037i 0x2cef018ec, 0x2cef0204c). The
// core loads of the tap's first sample were 0 on every core or 100 on one; a
// core's CPU_UserLoad and CPU_SystemLoad were -1 and its CPU_NiceLoad 0, and
// the CPU_TotalLoad of SystemCPUUsage was the sum of the cores' (iPadOS 27.3
// 24C5037i).
type UsageSample struct {
	Time      time.Time
	CoreLoads []float64
	System    []any
	Processes []ProcessUsage
}

type Sysmon struct {
	conn         atomic.Pointer[dtx.Conn]
	samples      chan UsageSample
	process      attributeOrder
	system       attributeOrder
	measured     UsageSample
	ended        uint64
	hasSystem    bool
	hasProcesses bool
	primed       bool
	origin       time.Time
	started      uint64
	stop         chan struct{}
	stopOnce     sync.Once
	access       sync.Mutex
	err          error
}

func DialSysmon(
	ctx, owner context.Context, device *coredevice.Device, interval time.Duration, processAttributes, systemAttributes []string,
) (*Sysmon, error) {
	conn, err := device.DialService(ctx, serviceName)
	if err != nil {
		return nil, E.Cause(err, "instruments: connect")
	}
	s := &Sysmon{
		samples: make(chan UsageSample, 4), stop: make(chan struct{}),
		process: attributeOrder{asked: processAttributes}, system: attributeOrder{asked: systemAttributes},
	}
	dtxConn, err := dtx.NewConn(owner, conn, s.handle)
	if err != nil {
		conn.Close()
		return nil, E.Cause(err, "instruments: initialize DTX")
	}
	s.conn.Store(dtxConn)
	err = dtxConn.Notify(ctx, 0, "_notifyOfPublishedCapabilities:", map[string]any{
		"com.apple.private.DTXBlockCompression": int64(0), "com.apple.private.DTXConnection": int64(1),
	})
	if err != nil {
		s.Close()
		return nil, E.Cause(err, "instruments: announce capabilities")
	}
	if _, err = dtxConn.Invoke(ctx, 0, "_requestChannelWithCode:identifier:", dtx.Int32(sysmonChannel), sysmonService); err != nil {
		s.Close()
		return nil, E.Cause(err, "instruments: open ", sysmonService)
	}
	err = dtxConn.Notify(ctx, sysmonChannel, "setConfig:", map[string]any{
		"ur": sysmonDelivery.Milliseconds(), "nohb": true, "bm": int64(0),
		"procAttrs": requested(processAttributes, "pid"), "sysAttrs": requested(systemAttributes, "physMemSize"),
		"cpuUsage": true, "sampleInterval": interval.Nanoseconds(),
	})
	if err == nil {
		err = dtxConn.Notify(ctx, sysmonChannel, "start")
	}
	if err != nil {
		s.Close()
		return nil, E.Cause(err, "instruments: start the system monitor tap")
	}
	return s, nil
}

func requested(attributes []string, required string) []any {
	names := common.Map(attributes, func(it string) any { return it })
	if !slices.Contains(attributes, required) {
		names = append(names, required)
	}
	return names
}

type attributeOrder struct {
	asked     []string
	positions []int
}

func (o *attributeOrder) report(names []any) {
	reported := make(map[string]int, len(names))
	for position, name := range names {
		if name, isString := name.(string); isString {
			reported[name] = position
		}
	}
	o.positions = make([]int, len(o.asked))
	for index, name := range o.asked {
		position, isReported := reported[name]
		if !isReported {
			position = -1
		}
		o.positions[index] = position
	}
}

func (o *attributeOrder) values(row []any) []any {
	values := make([]any, len(o.asked))
	for index := range values {
		position := index
		if o.positions != nil {
			position = o.positions[index]
		}
		if position >= 0 && position < len(row) {
			values[index] = row[position]
		}
	}
	return values
}

func (s *Sysmon) handle(event dtx.Event) (any, bool) {
	data, isData := event.Payload.([]byte)
	if !isData {
		return nil, true
	}
	decoded, err := dtx.UnmarshalArchive(data)
	if err != nil {
		s.fail(E.Cause(err, "instruments: decode the system monitor sample"))
		return nil, true
	}
	rows, _ := decoded.([]any)
	for _, value := range rows {
		row, _ := value.(map[string]any)
		if names, named := row["ProcessesAttributes"].([]any); named {
			s.process.report(names)
		}
		if names, named := row["SystemAttributes"].([]any); named {
			s.system.report(names)
		}
		if system, hasSystem := row["System"].([]any); hasSystem {
			s.measured.System, s.measured.CoreLoads, s.hasSystem = s.system.values(system), []float64{}, true
			if cores, _ := row["PerCPUUsage"].([]any); s.primed {
				s.measured.CoreLoads = common.Map(cores, load)
			}
			s.primed = true
		}
		switch reported := row["Processes"].(type) {
		case map[any]any, map[string]any:
			processes, _ := reported.(map[any]any)
			for pid, attributes := range processes {
				pid, isPID := unsigned(pid)
				if values, isList := attributes.([]any); isList && isPID {
					s.measured.Processes = append(s.measured.Processes, ProcessUsage{PID: pid, Values: s.process.values(values)})
				}
			}
			s.ended, _ = unsigned(row["EndMachAbsTime"])
			s.hasProcesses = true
		}
		if !s.hasSystem || !s.hasProcesses {
			continue
		}
		sample := s.measured
		s.measured, s.hasSystem, s.hasProcesses = UsageSample{}, false, false
		if s.origin.IsZero() {
			s.origin, s.started = time.Now(), s.ended
		}
		sample.Time = s.origin.Add(time.Duration(s.ended-s.started) * 125 / 3)
		select {
		case s.samples <- sample:
		case <-s.stop:
		}
	}
	return nil, true
}

func number(value any) float64 {
	switch value := value.(type) {
	case float64:
		return value
	case uint64:
		return float64(value)
	case int64:
		return float64(value)
	}
	return 0
}

func load(value any) float64 {
	loads, _ := value.(map[string]any)
	return number(loads["CPU_TotalLoad"])
}

func (s *Sysmon) fail(err error) {
	s.access.Lock()
	if s.err == nil {
		s.err = err
	}
	s.access.Unlock()
	s.Close()
}

func (s *Sysmon) Samples() <-chan UsageSample {
	return s.samples
}

func (s *Sysmon) Done() <-chan struct{} {
	return s.conn.Load().Done()
}

func (s *Sysmon) Err() error {
	s.access.Lock()
	err := s.err
	s.access.Unlock()
	if err != nil {
		return err
	}
	return s.conn.Load().Err()
}

func (s *Sysmon) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	if conn := s.conn.Load(); conn != nil {
		return conn.Close()
	}
	return nil
}
