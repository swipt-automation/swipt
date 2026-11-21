//go:build android && cgo

package automation

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/android"
)

func androidExec(ctx context.Context, call *operation) (any, error) {
	var encoded json.RawMessage
	var key string
	if err := call.decode(1, &encoded, &key); err != nil {
		return nil, err
	}
	var command android.Command
	if err := json.Unmarshal(encoded, &command); err != nil {
		return nil, E.Cause1(ErrInvalid, err)
	}
	var stdin io.Reader
	if key != "" {
		data, err := call.engine.takeBlob(key)
		if err != nil {
			return nil, err
		}
		stdin = bytes.NewReader(data)
	}
	result, err := android.Exec(ctx, command, stdin)
	if err != nil {
		return nil, androidError(err)
	}
	return map[string]any{
		"value": map[string]any{"exitCode": result.ExitCode, "stderr": result.Stderr},
		"data":  call.engine.putBlob(result.Stdout),
	}, nil
}

func androidSpawn(_ context.Context, call *operation) (any, error) {
	var command android.Command
	if err := call.androidParams(&command); err != nil {
		return nil, err
	}
	process, err := android.Spawn(command)
	if err != nil {
		return nil, androidError(err)
	}
	e := call.engine
	output := func(stream io.ReadCloser) *resource {
		return &resource{reader: stream, finish: func(error) error { return stream.Close() }}
	}
	return map[string]any{
		"pid":     process.PID,
		"process": e.addResource(&resource{handle: process, finish: func(error) error { return androidError(process.Kill()) }}),
		"stdin":   e.addResource(&resource{writer: process.Stdin, finish: func(error) error { return process.Stdin.Close() }}),
		"stdout":  e.addResource(output(process.Stdout)),
		"stderr":  e.addResource(output(process.Stderr)),
	}, nil
}

func androidWait(ctx context.Context, call *operation) (any, error) {
	var id string
	if err := call.decode(1, &id); err != nil {
		return nil, err
	}
	process, err := resourceHandle[*android.Process](call.engine, id, "a process")
	if err != nil {
		return nil, err
	}
	return process.Wait(ctx)
}

func androidKill(_ context.Context, call *operation) (any, error) {
	var id string
	var signal int
	if err := call.decode(2, &id, &signal); err != nil {
		return nil, err
	}
	process, err := resourceHandle[*android.Process](call.engine, id, "a process")
	if err != nil {
		return nil, err
	}
	return nil, androidError(process.Signal(signal))
}

func androidKillExited(_ context.Context, call *operation) (any, error) {
	var pid, signal int
	if err := call.decode(2, &pid, &signal); err != nil {
		return nil, err
	}
	if pid <= 0 {
		return nil, E.Cause1(ErrInvalid, E.New("pid must be positive"))
	}
	return nil, androidError(android.SignalExited(pid, signal))
}
