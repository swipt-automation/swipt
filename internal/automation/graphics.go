//go:build darwin && cgo

package automation

import (
	"context"
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/jsc"
)

type Graphics interface {
	Entry() uintptr
	Run(engine uint64, call int64, context *GraphicsCall) (json.RawMessage, error)
	Watch(engine uint64, call int64, context *GraphicsCall, publish func(any)) error
}

const (
	graphicsInstall int32 = iota
	graphicsUninstall
)

var (
	errNoGraphics   = E.New("graphics: this platform has no graphics")
	errStreamEnded  = E.New("the stream ended")
	graphicsEngines atomic.Uint64
)

var readOperations = []string{
	"device.info", "device.state", "device.installedApplications", "device.applicationIcon", "device.pasteboard",
	"accessibility.snapshot", "accessibility.find", "accessibility.changed",
}

type GraphicsCall struct {
	ctx  context.Context
	call *operation
}

func (c *GraphicsCall) Context() context.Context {
	return c.ctx
}

func (c *GraphicsCall) Screenshot() ([]byte, float64, error) {
	phone, err := c.call.screen(c.ctx)
	if err != nil {
		return nil, 0, err
	}
	attempted := time.Now()
	data, scale, err := phone.Capture(c.ctx)
	if c.call.dropped(c.ctx, err, attempted) {
		attempted = time.Now()
		data, scale, err = phone.Capture(c.ctx)
	}
	if c.call.dropped(c.ctx, err, attempted) {
		err = c.call.droppedError(err, "during the screenshot")
	}
	return data, scale, err
}

func (c *GraphicsCall) ReadFile(kind, name string) ([]byte, error) {
	at, err := c.call.engine.fileLocation(kind, name)
	if err != nil {
		return nil, err
	}
	defer at.root.Close()
	return at.root.read(at.name)
}

func (c *GraphicsCall) WriteFile(kind, name string, data []byte) error {
	at, err := c.call.engine.fileLocation(kind, name)
	if err != nil {
		return err
	}
	defer at.root.Close()
	return at.root.write(at.name, data)
}

func (c *GraphicsCall) StageFile(kind, name string) (*StagedFile, error) {
	at, err := c.call.engine.fileLocation(kind, name)
	if err != nil {
		return nil, err
	}
	defer at.root.Close()
	return at.root.stage(at.name)
}

func (c *GraphicsCall) Directory(kind, name string, create bool) (string, error) {
	at, err := c.call.engine.fileLocation(kind, name)
	if err != nil {
		return "", err
	}
	defer at.root.Close()
	if create {
		if err = at.root.mkdir(at.name); err != nil {
			return "", err
		}
	} else {
		info, found, err := at.root.stat(at.name)
		if err != nil {
			return "", err
		}
		if !found {
			return "", FileNotFound(at.root.where, at.name)
		}
		if !info.Directory {
			return "", at.root.error(at.name, &fs.PathError{Op: "open", Path: at.name, Err: syscall.ENOTDIR})
		}
	}
	return filepath.Join(at.root.root.Name(), at.name), nil
}

func (e *engine) installGraphics(native jsc.Value) {
	graphics := e.host.options.Graphics
	if graphics == nil {
		return
	}
	e.graphics = graphicsEngines.Add(1)
	e.context.RunNative(graphics.Entry(), graphicsInstall, native, e.graphics)
}

func (e *engine) uninstallGraphics() {
	graphics := e.host.options.Graphics
	if graphics == nil || e.graphics == 0 {
		return
	}
	e.context.RunNative(graphics.Entry(), graphicsUninstall, jsc.Value{}, e.graphics)
	e.graphics = 0
}

func graphicsRun(ctx context.Context, call *operation) (any, error) {
	var id int64
	if err := call.decode(1, &id); err != nil {
		return nil, err
	}
	graphics := call.engine.host.options.Graphics
	if graphics == nil {
		return nil, errNoGraphics
	}
	return graphics.Run(call.engine.graphics, id, &GraphicsCall{ctx: ctx, call: call})
}

func (e *engine) watchGraphics(ctx context.Context, call *operation, publish func(any, error), ended func(error)) error {
	var id int64
	if err := call.decode(1, &id); err != nil {
		return err
	}
	graphics := e.host.options.Graphics
	if graphics == nil {
		return errNoGraphics
	}
	go func() {
		err := graphics.Watch(e.graphics, id, &GraphicsCall{ctx: ctx, call: call}, func(value any) { publish(value, nil) })
		if err == nil {
			err = errStreamEnded
		}
		ended(err)
	}()
	return nil
}
