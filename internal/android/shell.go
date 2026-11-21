//go:build android

package android

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/fault"
)

const shellPath = "/system/bin/sh"

type Command struct {
	Command     string            `json:"command"`
	Arguments   []string          `json:"arguments"`
	Directory   string            `json:"directory"`
	Environment map[string]string `json:"environment"`
}

func (c Command) build() (*exec.Cmd, error) {
	var command *exec.Cmd
	switch {
	case c.Command != "" && len(c.Arguments) == 0:
		command = exec.Command(shellPath, "-c", c.Command)
	case c.Command == "" && len(c.Arguments) > 0:
		command = exec.Command(c.Arguments[0], c.Arguments[1:]...)
	default:
		return nil, E.Extend(fault.ErrInvalid, "a command is either a string or an array of arguments")
	}
	if command.Err != nil {
		return nil, E.Extend(fault.ErrNotFound, "no program ", c.Arguments[0])
	}
	command.Dir = c.Directory
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if len(c.Environment) > 0 {
		command.Env = os.Environ()
		for key, value := range c.Environment {
			command.Env = append(command.Env, key+"="+value)
		}
	}
	return command, nil
}

func startFailure(command *exec.Cmd, err error) error {
	if command.Dir != "" {
		if info, statErr := os.Stat(command.Dir); statErr != nil || !info.IsDir() {
			return E.Extend(fault.ErrNotFound, "no directory ", command.Dir)
		}
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
		return E.Extend(fault.ErrNotFound, "no program ", command.Path)
	}
	return err
}

type Result struct {
	ExitCode int
	Stdout   []byte
	Stderr   string
}

func Exec(ctx context.Context, request Command, stdin io.Reader) (Result, error) {
	command, err := request.build()
	if err != nil {
		return Result{}, err
	}
	var stdout, stderr bytes.Buffer
	command.Stdin, command.Stdout, command.Stderr = stdin, &stdout, &stderr
	if err = command.Start(); err != nil {
		return Result{}, startFailure(command, err)
	}
	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case err = <-waited:
	case <-ctx.Done():
		_ = signalGroup(command.Process.Pid, int(syscall.SIGKILL))
		return Result{}, ctx.Err()
	}
	exitCode, err := exitCodeOf(command, err)
	return Result{ExitCode: exitCode, Stdout: stdout.Bytes(), Stderr: stderr.String()}, err
}

func exitCodeOf(command *exec.Cmd, err error) (int, error) {
	if _, isExit := errors.AsType[*exec.ExitError](err); err != nil && !isExit {
		return 0, err
	}
	if status, isUnix := command.ProcessState.Sys().(syscall.WaitStatus); isUnix && status.Signaled() {
		return 128 + int(status.Signal()), nil
	}
	return command.ProcessState.ExitCode(), nil
}

type Process struct {
	PID    int
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	Stderr io.ReadCloser

	exited   chan struct{}
	exitCode int
	err      error
}

func Spawn(request Command) (*Process, error) {
	command, err := request.build()
	if err != nil {
		return nil, err
	}
	var pipes [3][2]*os.File
	closeAll := func() {
		for _, pipe := range pipes {
			for _, end := range pipe {
				if end != nil {
					_ = end.Close()
				}
			}
		}
	}
	for index := range pipes {
		if pipes[index][0], pipes[index][1], err = os.Pipe(); err != nil {
			closeAll()
			return nil, err
		}
	}
	command.Stdin, command.Stdout, command.Stderr = pipes[0][0], pipes[1][1], pipes[2][1]
	if err = command.Start(); err != nil {
		closeAll()
		return nil, startFailure(command, err)
	}
	_ = pipes[0][0].Close()
	_ = pipes[1][1].Close()
	_ = pipes[2][1].Close()
	process := &Process{PID: command.Process.Pid, Stdin: pipes[0][1], Stdout: pipes[1][0], Stderr: pipes[2][0], exited: make(chan struct{})}
	go func() {
		process.exitCode, process.err = exitCodeOf(command, command.Wait())
		close(process.exited)
	}()
	return process, nil
}

func (p *Process) Wait(ctx context.Context) (int, error) {
	select {
	case <-p.exited:
		return p.exitCode, p.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

func (p *Process) Signal(signal int) error {
	select {
	case <-p.exited:
		return SignalExited(p.PID, signal)
	default:
	}
	return signalGroup(p.PID, signal)
}

func SignalExited(pid int, signal int) error {
	if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
		return nil
	}
	return signalGroup(pid, signal)
}

func signalGroup(pid int, signal int) error {
	err := syscall.Kill(-pid, syscall.Signal(signal))
	switch {
	case err == nil, errors.Is(err, syscall.ESRCH):
		return nil
	case errors.Is(err, syscall.EPERM):
		return &Error{Name: "NotAllowedError", Message: "no permission to signal the processes of " + strconv.Itoa(pid) + ", which run as another user"}
	default:
		return err
	}
}

func (p *Process) Kill() error {
	return p.Signal(int(syscall.SIGKILL))
}
