//go:build android

package android

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	E "github.com/sagernet/sing/common/exceptions"
)

const (
	shellUID         = 2000
	shellHelperClass = "io.nekohasekai.swipt.device.ShellHelper"
)

type shellHelper struct {
	apk    string
	access sync.Mutex
	proc   *shellHelperProcess
	closed bool
}

type shellHelperProcess struct {
	command             *exec.Cmd
	input               io.WriteCloser
	writing             sync.Mutex
	access              sync.Mutex
	next                int64
	pending             map[int64]chan shellReply
	clipboardAccess     chan struct{}
	clipboardWatchers   map[chan struct{}]struct{}
	advertiseSession    int64
	onAdvertiseHostname func(string)
	onAdvertiseError    func(error)
	events              []func()
	eventsQueued        chan struct{}
	exited              chan struct{}
	failure             error
}

type shellRequest struct {
	ID         int64    `json:"id"`
	Method     string   `json:"method"`
	Text       *string  `json:"text,omitempty"`
	Session    int64    `json:"session,omitempty"`
	Name       string   `json:"name,omitempty"`
	Type       string   `json:"type,omitempty"`
	Port       int      `json:"port,omitempty"`
	Hostname   string   `json:"hostname,omitempty"`
	Attributes []string `json:"attributes,omitempty"`
}

type shellReply struct {
	ID       int64           `json:"id"`
	Value    json.RawMessage `json:"value"`
	Error    string          `json:"error"`
	Event    string          `json:"event"`
	Session  int64           `json:"session"`
	Hostname string          `json:"hostname"`
}

func (h *shellHelper) started() (*shellHelperProcess, error) {
	h.access.Lock()
	defer h.access.Unlock()
	if h.closed {
		return nil, E.New("the shell helper closed")
	}
	if h.proc != nil {
		select {
		case <-h.proc.exited:
		default:
			return h.proc, nil
		}
	}
	command := exec.Command("/system/bin/app_process", "/system/bin", shellHelperClass)
	command.Env = append(os.Environ(), "CLASSPATH="+h.apk)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: shellUID, Gid: shellUID}}
	input, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	output, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var failure bytes.Buffer
	command.Stderr = &failure
	if err = command.Start(); err != nil {
		return nil, E.Cause(err, "start the shell helper")
	}
	proc := &shellHelperProcess{
		command:           command,
		input:             input,
		pending:           make(map[int64]chan shellReply),
		clipboardAccess:   make(chan struct{}, 1),
		clipboardWatchers: make(map[chan struct{}]struct{}),
		eventsQueued:      make(chan struct{}, 1),
		exited:            make(chan struct{}),
	}
	go proc.deliver()
	go func() {
		lines := bufio.NewScanner(output)
		lines.Buffer(nil, 64<<20)
		for lines.Scan() {
			var reply shellReply
			if json.Unmarshal(lines.Bytes(), &reply) != nil {
				continue
			}
			proc.dispatch(reply)
		}
		err := command.Wait()
		if message := strings.TrimSpace(failure.String()); message != "" {
			err = E.New(message)
		}
		if err == nil {
			err = E.New("the shell helper exited")
		} else {
			err = E.Cause(err, "the shell helper exited")
		}
		proc.access.Lock()
		proc.failure = err
		close(proc.exited)
		if onAdvertiseError := proc.onAdvertiseError; onAdvertiseError != nil {
			proc.queue(func() { onAdvertiseError(err) })
		}
		proc.onAdvertiseHostname, proc.onAdvertiseError = nil, nil
		proc.access.Unlock()
		close(proc.eventsQueued)
	}()
	h.proc = proc
	return proc, nil
}

func (h *shellHelper) close() {
	h.access.Lock()
	h.closed = true
	proc := h.proc
	h.proc = nil
	h.access.Unlock()
	if proc == nil {
		return
	}
	proc.access.Lock()
	proc.onAdvertiseHostname, proc.onAdvertiseError = nil, nil
	proc.access.Unlock()
	_ = proc.input.Close()
	_ = proc.command.Process.Kill()
	<-proc.exited
}

func (p *shellHelperProcess) dispatch(reply shellReply) {
	p.access.Lock()
	defer p.access.Unlock()
	switch reply.Event {
	case "clipboard":
		for watcher := range p.clipboardWatchers {
			select {
			case watcher <- struct{}{}:
			default:
			}
		}
	case "advertise.hostname":
		if onHostname := p.onAdvertiseHostname; onHostname != nil && reply.Session == p.advertiseSession {
			p.queue(func() { onHostname(reply.Hostname) })
		}
	case "advertise.error":
		if onError := p.onAdvertiseError; onError != nil && reply.Session == p.advertiseSession {
			p.onAdvertiseHostname, p.onAdvertiseError = nil, nil
			p.queue(func() { onError(Failure(E.New(reply.Error))) })
		}
	default:
		if waiting, found := p.pending[reply.ID]; found {
			delete(p.pending, reply.ID)
			waiting <- reply
		}
	}
}

func (p *shellHelperProcess) queue(event func()) {
	p.events = append(p.events, event)
	select {
	case p.eventsQueued <- struct{}{}:
	default:
	}
}

func (p *shellHelperProcess) deliver() {
	for range p.eventsQueued {
		p.access.Lock()
		events := p.events
		p.events = nil
		p.access.Unlock()
		for _, event := range events {
			event()
		}
	}
}

func (p *shellHelperProcess) call(ctx context.Context, request shellRequest) (json.RawMessage, error) {
	replied := make(chan shellReply, 1)
	p.access.Lock()
	p.next++
	request.ID = p.next
	p.pending[request.ID] = replied
	p.access.Unlock()
	defer func() {
		p.access.Lock()
		delete(p.pending, request.ID)
		p.access.Unlock()
	}()
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	p.writing.Lock()
	_, err = p.input.Write(append(encoded, '\n'))
	p.writing.Unlock()
	if err != nil {
		<-p.exited
		return nil, p.failure
	}
	select {
	case reply := <-replied:
		if reply.Error != "" {
			return nil, Failure(E.New(reply.Error))
		}
		return reply.Value, nil
	case <-p.exited:
		return nil, p.failure
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
