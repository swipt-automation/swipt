//go:build cgo

package automation

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/jsc"
)

type Graphics interface {
	Install(context *jsc.Context, native jsc.Value) GraphicsEngine
}

type GraphicsEngine interface {
	Run(call int64, context *GraphicsCall) (json.RawMessage, error)
	Watch(call int64, context *GraphicsCall, publish func(any)) error
	Close()
}

var (
	errNoGraphics  = E.New("graphics: this platform has no graphics")
	errStreamEnded = E.New("the stream ended")
)

var readOperations = []string{
	"device.info", "device.state", "device.installedApplications", "device.applicationIcon", "device.pasteboard", "process.list",
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
			return "", E.Cause1(ErrInvalid, E.New(strconv.Quote(filepath.ToSlash(at.name)), " is a file in ", at.root.where))
		}
	}
	return filepath.Join(at.root.root.Name(), at.name), nil
}

func (e *engine) installGraphics(native jsc.Value) {
	graphics := e.host.options.Graphics
	if graphics == nil {
		return
	}
	e.graphics = graphics.Install(e.context, native)
}

func (e *engine) uninstallGraphics() {
	if e.graphics == nil {
		return
	}
	e.graphics.Close()
	e.graphics = nil
}

func graphicsRun(ctx context.Context, call *operation) (any, error) {
	var id int64
	if err := call.decode(1, &id); err != nil {
		return nil, err
	}
	graphics := call.engine.graphics
	if graphics == nil {
		return nil, errNoGraphics
	}
	return graphics.Run(id, &GraphicsCall{ctx: ctx, call: call})
}

func watchGraphics(ctx context.Context, call *operation, publish func(any), ended func(error)) error {
	var id int64
	if err := call.decode(1, &id); err != nil {
		return err
	}
	graphics := call.engine.graphics
	if graphics == nil {
		return errNoGraphics
	}
	go func() {
		err := graphics.Watch(id, &GraphicsCall{ctx: ctx, call: call}, publish)
		if err == nil {
			err = errStreamEnded
		}
		ended(err)
	}()
	return nil
}
