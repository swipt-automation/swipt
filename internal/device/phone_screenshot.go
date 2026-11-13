package device

import (
	"context"

	"github.com/swipt-automation/swipt/internal/coredevice/screencapture"
)

// dtscreencaptured rejects a request made while another capture is in
// progress (DDI 27B48 dtscreencaptured 0x10000ee80, 0x10000f048).
func (p *Phone) Capture(ctx context.Context) ([]byte, float64, error) {
	c, ctx, done, err := p.begin(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer done()
	geometry, err := c.readGeometry(ctx)
	if err != nil {
		return nil, 0, err
	}
	p.captureAccess.Lock()
	defer p.captureAccess.Unlock()
	data, err := screencapture.Capture(ctx, c.device)
	return data, geometry.Scale, err
}
