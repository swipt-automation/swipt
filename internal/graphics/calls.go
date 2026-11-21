//go:build cgo

package graphics

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/swipt-automation/swipt/internal/automation"
)

type colorCheck struct {
	Point     []float64 `json:"point"`
	Color     []int     `json:"color"`
	Tolerance *int      `json:"tolerance"`
}

type registrar struct {
	engine *engine

	access    sync.Mutex
	made      []int64
	abandoned bool
}

func (r *registrar) register(object any) int64 {
	r.access.Lock()
	defer r.access.Unlock()
	if r.abandoned {
		release(object)
		return 0
	}
	identifier := r.engine.register(object)
	r.made = append(r.made, identifier)
	return identifier
}

func (r *registrar) abandon() {
	r.access.Lock()
	r.abandoned = true
	made := r.made
	r.access.Unlock()
	for _, identifier := range made {
		release(r.engine.take(identifier))
	}
}

type imageCall struct {
	*call
	arguments callArguments
	context   *automation.GraphicsCall
	ctx       context.Context
	registrar *registrar
}

type outcome struct {
	value any
	err   error
}

func (e *engine) runImage(c *call, arguments callArguments, context *automation.GraphicsCall) (any, error) {
	owned := c.owned
	c.owned = nil
	current := &imageCall{call: c, arguments: arguments, context: context, ctx: context.Context(), registrar: &registrar{engine: e}}
	done := make(chan outcome, 1)
	go func() {
		value, err := current.run()
		for _, object := range owned {
			object.release()
		}
		done <- outcome{value, err}
	}()
	select {
	case result := <-done:
		return result.value, result.err
	case <-current.ctx.Done():
		current.registrar.abandon()
		return nil, current.ctx.Err()
	}
}

func (c *imageCall) run() (any, error) {
	switch c.method {
	case "screenshot":
		return c.screenshot()
	case "decode":
		return c.decode()
	case "encode":
		data, err := c.encoded()
		if err != nil {
			return nil, err
		}
		return map[string]int64{"bytes": c.registrar.register(data)}, nil
	case "save":
		data, err := c.encoded()
		if err != nil {
			return nil, err
		}
		return struct{}{}, c.context.WriteFile(c.arguments.Root, c.arguments.Path, data)
	case "text", "barcodes", "distance", "nearest":
		return c.vision()
	}
	return c.analyze()
}

func (c *imageCall) image(index int) (*imageNode, error) {
	if index < len(c.objects) {
		if found, isImage := c.objects[index].(*imageNode); isImage {
			return found, nil
		}
	}
	return nil, failure(nameType, "graphics: argument ", index+1, " of ", c.method, " is not an Image")
}

func (c *imageCall) registered(node *imageNode, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return map[string]int64{"image": c.registrar.register(node)}, nil
}

func (c *imageCall) screenshot() (any, error) {
	started := milliseconds(time.Now())
	data, scale, err := c.context.Screenshot(c.arguments.Display)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, failure(nameUnavailable, "graphics: the device sent no screenshot")
	}
	if err = c.ctx.Err(); err != nil {
		return nil, err
	}
	var given *float64
	if scale > 0 {
		given = &scale
	}
	return c.registered(decodedNode(data, given, started))
}

func (c *imageCall) decode() (any, error) {
	if data, isBytes := c.first().([]byte); isBytes {
		return c.registered(decodedNode(data, c.arguments.Scale, math.NaN()))
	}
	if c.arguments.Path == "" {
		return nil, failure(nameType, "decode needs bytes or a file")
	}
	data, err := c.context.ReadFile(c.arguments.Root, c.arguments.Path)
	if err != nil {
		return nil, err
	}
	scale := c.arguments.Scale
	if scale == nil {
		scale = fileScale(c.arguments.Path)
	}
	return c.registered(decodedNode(data, scale, math.NaN()))
}

func (c *imageCall) encoded() ([]byte, error) {
	node, err := c.image(0)
	if err != nil {
		return nil, err
	}
	format := c.arguments.Format
	switch format {
	case "":
		format = "png"
	case "png", "jpeg", "heic":
	default:
		return nil, failure(nameType, "the format must be png, jpeg or heic")
	}
	return encodeImage(c.ctx, node, format, c.arguments.Quality)
}
