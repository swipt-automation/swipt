//go:build cgo && (darwin || android)

package graphics

import "context"

func (s *encodedSource) render(ctx context.Context) (*raster, error) {
	return decodePixels(ctx, s.data, s.header)
}

func (s *encodedSource) release() {}

func encodeImage(ctx context.Context, node *imageNode, format string, quality *float64) ([]byte, error) {
	shown, err := node.raster(ctx)
	if err != nil {
		return nil, err
	}
	return encodePixels(shown, format, quality, node.scale())
}
