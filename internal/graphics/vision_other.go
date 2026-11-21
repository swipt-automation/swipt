//go:build cgo && !darwin

package graphics

import "github.com/swipt-automation/swipt/internal/log"

func unsupported(name string) error {
	return failure(nameNotSupported, "graphics: ", name, " is not available on this platform")
}

func warmVision(log.ContextLogger) {}

func (c *imageCall) vision() (any, error) {
	name, renamed := map[string]string{"text": "recognizeText", "barcodes": "detectBarcodes"}[c.method]
	if !renamed {
		name = c.method
	}
	return nil, unsupported(name)
}
