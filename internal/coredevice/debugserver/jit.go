package debugserver

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
)

// StikDebug's universal script answers `brk #0xf00d` by the command in x16:
// it steps over the breakpoint, then 0 detaches, and 1 writes 0x69 to every
// 16 KiB page of the x1 bytes from x0, 128 writes at a time, and answers the
// address in x0 (StikDebug 63713ea, StikDebug/Scripts/universal.js:24-31,
// 116-118, 130-134, 178-207; StikDebug/JSSupport/JSDebugSupport.swift:104-165).
// Kernarium asks the same of a debugger and reads x0 0 as a refusal (Kernarium
// 9bda5e8, darwin/cpu/jit_memory/executable_memory.cpp:245-281).
const (
	requestWord    = 0xd43e01a0
	commandDetach  = 0
	commandPrepare = 1
	preparedPage   = 16 << 10
	preparedBatch  = 128

	registerX0  = 0
	registerX1  = 1
	registerX16 = 16
	registerPC  = 32

	signalTrap = 5
)

type JIT struct {
	conn *Conn
}

func AttachJIT(ctx context.Context, device *coredevice.Device, pid uint64) (*JIT, error) {
	conn, err := Dial(ctx, device)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	_, err = conn.Resume("vAttach;" + strconv.FormatUint(pid, 16))
	if !stop() && err == nil {
		err = ctx.Err()
	}
	if err != nil {
		conn.Close()
		return nil, E.Errors(ctx.Err(), E.Cause(err, "debugserver: attach to process ", pid))
	}
	return &JIT{conn: conn}, nil
}

func (j *JIT) Serve(ctx context.Context) error {
	defer j.conn.Close()
	stopClosing := context.AfterFunc(ctx, func() { j.conn.Close() })
	defer stopClosing()
	stop, err := j.conn.Continue("c")
	for err == nil {
		packet := fmt.Sprintf("C%02x", stop.Signal)
		if stop.Signal == signalTrap {
			var answered, detached bool
			answered, detached, err = j.answer(stop)
			if err != nil || detached {
				break
			}
			if answered {
				packet = "c"
			}
		}
		stop, err = j.conn.Continue(packet)
	}
	if exited := (*ExitedError)(nil); errors.As(err, &exited) {
		return nil
	}
	return E.Errors(ctx.Err(), err)
}

func (j *JIT) answer(stop Stop) (answered, detached bool, err error) {
	pc, err := stop.Register(registerPC)
	if err != nil {
		return false, false, err
	}
	word, err := j.conn.ReadMemory(pc, 4)
	if err != nil {
		return false, false, err
	}
	if binary.LittleEndian.Uint32(word) != requestWord {
		return false, false, nil
	}
	command, err := stop.Register(registerX16)
	if err != nil {
		return false, false, err
	}
	step := WriteRegister(stop.Thread, registerPC, pc+4)
	switch command {
	case commandDetach:
		return true, true, j.conn.Commands(step, "D")
	case commandPrepare:
		address, err := stop.Register(registerX0)
		if err != nil {
			return false, false, err
		}
		length, err := stop.Register(registerX1)
		if err != nil {
			return false, false, err
		}
		err = j.prepare(address, length)
		if replyErr := (*ReplyError)(nil); errors.As(err, &replyErr) {
			address, err = 0, nil
		}
		if err != nil {
			return false, false, err
		}
		return true, false, j.conn.Commands(step, WriteRegister(stop.Thread, registerX0, address))
	default:
		return true, false, j.conn.Commands(step)
	}
}

func (j *JIT) prepare(address, length uint64) error {
	var packets []string
	for offset := uint64(0); offset < length; offset += preparedPage {
		packets = append(packets, fmt.Sprintf("M%x,1:69", address+offset))
	}
	for batch := range slices.Chunk(packets, preparedBatch) {
		if err := j.conn.Commands(batch...); err != nil {
			return err
		}
	}
	return nil
}
