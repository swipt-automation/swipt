//go:build !android && cgo

package graphics

import "time"

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
