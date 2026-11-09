package instruments

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/dtx"
)

const (
	activityService = "com.apple.instruments.server.services.activitytracetap"
	activityChannel = 1
)

// The configuration follows pymobiledevice3 9537c0a,
// pymobiledevice3/services/dvt/instruments/activity_trace_tap.py:370-385,
// with the caller's predicate and an update rate of 10 instead of 500: a
// record of runningboardd's came 7 to 40 ms after it was logged, against 23
// to 73 ms at 50 and no sooner at 1. Without a predicate, the tap sent the
// log of every process whatever the target process, 173 MB in 75 s (iPad17,1,
// iPadOS 27.3 24C5037i).
func activityConfig(predicate string) map[string]any {
	return map[string]any{
		"bm": int64(0), "combineDataScope": int64(0), "machTimebaseDenom": int64(3), "machTimebaseNumer": int64(125),
		"onlySignposts": int64(0), "pidToInjectCombineDYLIB": "-1", "predicate": predicate, "signpostsAndLogs": int64(1),
		"trackPidToExecNameMapping": true, "enableHTTPArchiveLogging": false, "targetPID": int64(-3),
		"trackExpiredPIDs": int64(1), "ur": int64(10),
	}
}

// Continuous is the mach continuous time the record was logged at, and At the
// time on the device's clock then.
type LogEntry struct {
	PID        uint64
	At         time.Time
	Continuous uint64
	Message    string
}

type LogStream struct {
	conn      atomic.Pointer[dtx.Conn]
	entries   chan LogEntry
	ready     chan struct{}
	readyOnce sync.Once
	stop      chan struct{}
	stopOnce  sync.Once
	decoder   activityDecoder
	clock     atomic.Pointer[DeviceClock]
	access    sync.Mutex
	err       error
}

// DialLogStream streams the log records the predicate selects; Ready closes
// once the tap sends its first records or heartbeat.
func DialLogStream(ctx, owner context.Context, device *coredevice.Device, predicate string) (*LogStream, error) {
	conn, err := device.DialService(ctx, serviceName)
	if err != nil {
		return nil, E.Cause(err, "instruments: connect")
	}
	s := &LogStream{entries: make(chan LogEntry, 64), ready: make(chan struct{}), stop: make(chan struct{})}
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
	if _, err = dtxConn.Invoke(ctx, 0, "_requestChannelWithCode:identifier:", dtx.Int32(activityChannel), activityService); err != nil {
		s.Close()
		return nil, E.Cause(err, "instruments: open ", activityService)
	}
	if err = dtxConn.Notify(ctx, activityChannel, "setConfig:", activityConfig(predicate)); err == nil {
		err = dtxConn.Notify(ctx, activityChannel, "start")
	}
	if err != nil {
		s.Close()
		return nil, E.Cause(err, "instruments: start the activity trace tap")
	}
	return s, nil
}

func (s *LogStream) handle(event dtx.Event) (any, bool) {
	switch payload := event.Payload.(type) {
	case []byte:
		s.readyOnce.Do(func() { close(s.ready) })
		err := s.decoder.feed(payload, func(entry LogEntry) bool {
			select {
			case s.entries <- entry:
				return true
			case <-s.stop:
				return false
			}
		})
		if err != nil {
			s.fail(E.Cause(err, "instruments: decode the activity trace"))
		}
	case map[string]any:
		message, _ := payload["DTTapMessagePlist"].(map[string]any)
		if _, heartbeat := message["heart"]; heartbeat {
			s.readyOnce.Do(func() { close(s.ready) })
		}
		s.decoder.notice(message)
		if s.decoder.clock != nil {
			s.clock.Store(s.decoder.clock)
		}
	}
	return nil, true
}

func (s *LogStream) fail(err error) {
	s.access.Lock()
	if s.err == nil {
		s.err = err
	}
	s.access.Unlock()
	s.Close()
}

func (s *LogStream) Ready() <-chan struct{} {
	return s.ready
}

// Clock is the device's clock as the tap last gave it.
func (s *LogStream) Clock() *DeviceClock {
	return s.clock.Load()
}

func (s *LogStream) Entries() <-chan LogEntry {
	return s.entries
}

func (s *LogStream) Done() <-chan struct{} {
	return s.conn.Load().Done()
}

func (s *LogStream) Err() error {
	s.access.Lock()
	err := s.err
	s.access.Unlock()
	if err != nil {
		return err
	}
	return s.conn.Load().Err()
}

func (s *LogStream) Close() error {
	s.stopOnce.Do(func() { close(s.stop) })
	if conn := s.conn.Load(); conn != nil {
		return conn.Close()
	}
	return nil
}

// The tap sends a stack machine's program of little-endian 16-bit words: an
// opcode in the high byte, or a pushed value as 14-bit chunks, big-endian,
// all but the last marked 0b10 and the last 0b11 in the top bits; a record
// may end in one DATA message and go on in the next (pymobiledevice3 9537c0a,
// pymobiledevice3/services/dvt/instruments/activity_trace_tap.py:20-28,
// 398-613).
const (
	opDefineTable           = 0x01
	opEndRow                = 0x02
	opConvertMachContinuous = 0x05
	opTableReset            = 0x64
	opCopy                  = 0x65
	opSentinel              = 0x68
	opStruct                = 0x69
	opPlaceholderCount      = 0x6A
	opDebug                 = 0x6B
)

type activityTable struct {
	columns []string
	process int
	time    int
	message int
}

// DeviceClock maps the device's mach continuous time to its wall clock.
type DeviceClock struct {
	continuous             uint64
	since1970              float64
	numerator, denominator uint64
}

type activityDecoder struct {
	stack  []any
	tables []activityTable
	carry  []byte
	clock  *DeviceClock
}

// The "Time Mapping" notice gives the device's wall clock at one mach
// continuous time, which the records are stamped in (pymobiledevice3 9537c0a,
// pymobiledevice3/services/dvt/instruments/activity_trace_tap.py:74-97,
// 409-417).
func (d *activityDecoder) notice(message map[string]any) {
	if message["notice"] != "Time Mapping" {
		return
	}
	info, _ := message["info"].(map[string]any)
	continuous, known := unsigned(info["DTActivityTraceTapDeviceMachContinuousTimeKey"])
	numerator, numeratorKnown := unsigned(info["DTActivityTraceTapDeviceMachTimebaseNumeratorKey"])
	denominator, denominatorKnown := unsigned(info["DTActivityTraceTapDeviceMachTimebaseDenominatorKey"])
	var since1970 float64
	switch value := info["DTActivityTraceTapDeviceTimeIntervalSince1970Key"].(type) {
	case float64:
		since1970 = value
	default:
		if whole, ok := unsigned(value); ok {
			since1970 = float64(whole)
		} else {
			known = false
		}
	}
	if known && numeratorKnown && denominatorKnown && numerator > 0 && denominator > 0 {
		d.clock = &DeviceClock{continuous: continuous, since1970: since1970, numerator: numerator, denominator: denominator}
	}
}

func (c DeviceClock) Time(continuous uint64) time.Time {
	ticks := int64(continuous - c.continuous)
	seconds, fraction := math.Modf(c.since1970)
	nanoseconds := fraction*1e9 + float64(ticks)*float64(c.numerator)/float64(c.denominator)
	return time.Unix(int64(seconds), int64(nanoseconds))
}

func unsigned(value any) (uint64, bool) {
	switch value := value.(type) {
	case uint64:
		return value, true
	case int64:
		return uint64(value), value >= 0
	}
	return 0, false
}

func (d *activityDecoder) feed(data []byte, emit func(LogEntry) bool) error {
	program := data
	if len(d.carry) > 0 {
		program = append(d.carry, data...)
		d.carry = nil
	}
	for position := 0; ; {
		if len(program)-position < 2 {
			d.carry = append([]byte(nil), program[position:]...)
			return nil
		}
		word := binary.LittleEndian.Uint16(program[position:])
		var err error
		switch word >> 8 {
		case opTableReset:
			d.stack = d.stack[:0]
		case opSentinel:
			d.stack = append(d.stack, nil)
		case opStruct:
			err = d.group(int(word & 0xFF))
		case opDefineTable:
			err = d.defineTable()
		case opDebug:
			_, err = d.pop(1)
		case opCopy:
			err = d.copy(int(word & 0xFF))
		case opPlaceholderCount:
			_, err = d.pop(int(word & 0xFF))
		case opConvertMachContinuous:
		case opEndRow:
			var entry LogEntry
			var found bool
			entry, found, err = d.endRow(int(word & 0xFF))
			if err == nil && found && !emit(entry) {
				return nil
			}
		default:
			value, next, complete, pushErr := push(program, position)
			if pushErr != nil {
				return pushErr
			}
			if !complete {
				d.carry = append([]byte(nil), program[position:]...)
				return nil
			}
			d.stack = append(d.stack, value)
			position = next
			continue
		}
		if err != nil {
			return err
		}
		position += 2
	}
}

// push reads the value whose first word is at position, padded with zero
// bits to whole bytes, and a whole byte more when its bits already fill them.
func push(program []byte, position int) (value []byte, next int, complete bool, err error) {
	var chunks []uint16
	for {
		if len(program)-position < 2 {
			return nil, 0, false, nil
		}
		word := binary.LittleEndian.Uint16(program[position:])
		position += 2
		if len(chunks) == 0 && word>>14 != 0b10 && word>>14 != 0b11 {
			return nil, 0, false, E.New("unknown opcode 0x", strconv.FormatUint(uint64(word>>8), 16))
		}
		chunks = append(chunks, word&0x3FFF)
		if word>>14 == 0b11 {
			break
		}
	}
	bits := 14 * len(chunks)
	value = make([]byte, (bits+8-bits%8)/8)
	bit := 0
	for _, chunk := range chunks {
		for shift := 13; shift >= 0; shift-- {
			if chunk>>shift&1 == 1 {
				value[bit/8] |= 0x80 >> (bit % 8)
			}
			bit++
		}
	}
	return value, position, true, nil
}

func (d *activityDecoder) pop(count int) ([]any, error) {
	if count > len(d.stack) {
		return nil, E.New("the stack holds ", len(d.stack), " items, not ", count)
	}
	items := d.stack[len(d.stack)-count:]
	d.stack = d.stack[:len(d.stack)-count]
	return items, nil
}

func (d *activityDecoder) group(count int) error {
	if count == 0xFF {
		return E.New("long struct")
	}
	items, err := d.pop(count)
	if err != nil {
		return err
	}
	d.stack = append(d.stack, append([]any(nil), items...))
	return nil
}

func (d *activityDecoder) copy(distance int) error {
	if distance == 0xFF {
		items, err := d.pop(1)
		if err != nil {
			return err
		}
		reference := int(littleEndian(items[0])) - 1
		if reference < 0 || reference >= len(d.stack) {
			return E.New("copy of item ", reference, " of ", len(d.stack))
		}
		d.stack = append(d.stack, d.stack[reference])
		return nil
	}
	if distance+1 > len(d.stack) {
		return E.New("copy of item ", distance, " back of ", len(d.stack))
	}
	d.stack = append(d.stack, d.stack[len(d.stack)-distance-1])
	return nil
}

func (d *activityDecoder) defineTable() error {
	items, err := d.pop(4)
	if err != nil {
		return err
	}
	columns, _ := items[3].([]any)
	table := activityTable{process: -1, time: -1, message: -1}
	for index, column := range columns {
		name := cString(column)
		table.columns = append(table.columns, name)
		switch name {
		case "process":
			table.process = index
		case "time":
			table.time = index
		case "message":
			table.message = index
		}
	}
	d.tables = append(d.tables, table)
	return nil
}

func (d *activityDecoder) endRow(index int) (LogEntry, bool, error) {
	if index >= len(d.tables) {
		return LogEntry{}, false, E.New("row of table ", index, " of ", len(d.tables))
	}
	table := d.tables[index]
	row, err := d.pop(len(table.columns))
	if err != nil || table.message < 0 {
		return LogEntry{}, false, err
	}
	entry := LogEntry{At: time.Now()}
	if table.process >= 0 {
		if process, _ := row[table.process].([]any); len(process) > 0 {
			entry.PID = littleEndian(process[0])
		}
	}
	if table.time >= 0 {
		if stamp, isBytes := row[table.time].([]byte); isBytes {
			entry.Continuous = littleEndian(stamp)
			if d.clock != nil {
				entry.At = d.clock.Time(entry.Continuous)
			}
		}
	}
	items, _ := row[table.message].([]any)
	entry.Message = compose(items)
	return entry, true, nil
}

// compose joins a record's narrative text and its string, private and
// integer arguments, which is all the records read here hold; any other
// argument shows as its type (pymobiledevice3 9537c0a,
// pymobiledevice3/services/dvt/instruments/activity_trace_tap.py:216-302).
func compose(items []any) string {
	var builder strings.Builder
	for _, item := range items {
		pair, _ := item.([]any)
		if len(pair) < 2 {
			continue
		}
		kind := cString(pair[0])
		data, _ := pair[1].([]byte)
		switch {
		case kind == "narrative-text":
			builder.WriteString(strings.ReplaceAll(cString(data), "%%", "%"))
		case kind == "string":
			builder.WriteString(cString(data))
		case kind == "private":
			builder.WriteString("<private>")
		case strings.HasPrefix(kind, "uint64") && strings.Contains(kind, "hex"):
			builder.WriteString(strconv.FormatUint(littleEndian(data), 16))
		case strings.HasPrefix(kind, "uint64"):
			builder.WriteString(strconv.FormatUint(littleEndian(data), 10))
		default:
			builder.WriteString("<" + kind + ">")
		}
	}
	return strings.TrimRight(builder.String(), " \t\n")
}

func cString(value any) string {
	data, _ := value.([]byte)
	if end := bytes.IndexByte(data, 0); end >= 0 {
		data = data[:end]
	}
	return string(data)
}

func littleEndian(value any) uint64 {
	data, _ := value.([]byte)
	var padded [8]byte
	copy(padded[:], data)
	return binary.LittleEndian.Uint64(padded[:])
}
