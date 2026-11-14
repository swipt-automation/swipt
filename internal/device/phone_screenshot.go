package device

import (
	"bytes"
	"context"
	"image"
	"image/draw"
	"image/png"

	E "github.com/sagernet/sing/common/exceptions"
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
	data, err := screencapture.Capture(ctx, c.device, geometry.display.UniqueID)
	if err != nil {
		return nil, 0, err
	}
	// dtscreencaptured turns the picture by the sum of the display's
	// currentOrientation and nativeOrientation, in quarter turns (DDI 27B52
	// dtscreencaptured 0x100010f4c..0x100010f94, 0x100012588). The picture of
	// the inner display of an iPhone Duo at rot90 comes unturned, a quarter turn
	// counterclockwise of its interface (iOS 27.2 24B91020).
	shown := geometry.display
	if turns := orientationTurns(shown.CurrentOrientation); turns%2 == 1 && shown.Width != shown.Height {
		picture, err := png.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, 0, E.Cause(err, "read the screenshot")
		}
		if picture.Width == shown.Width && picture.Height == shown.Height {
			data, err = turnPicture(data, turns)
			if err != nil {
				return nil, 0, E.Cause(err, "turn the screenshot")
			}
		}
	}
	return data, geometry.Scale, nil
}

func turnPicture(data []byte, turns int) ([]byte, error) {
	decoded, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	source := image.NewRGBA(decoded.Bounds())
	draw.Draw(source, source.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	width, height := source.Rect.Dx(), source.Rect.Dy()
	turned := image.NewRGBA(image.Rect(0, 0, height, width))
	for y := range height {
		for x := range width {
			toX, toY := height-1-y, x
			if turns == 3 {
				toX, toY = y, width-1-x
			}
			copy(turned.Pix[turned.PixOffset(toX, toY):][:4], source.Pix[source.PixOffset(source.Rect.Min.X+x, source.Rect.Min.Y+y):][:4])
		}
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err = encoder.Encode(&encoded, turned); err != nil {
		return nil, err
	}
	return encoded.Bytes(), nil
}
