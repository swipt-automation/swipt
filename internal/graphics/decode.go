//go:build cgo

package graphics

import (
	"math"
	"path/filepath"
	"regexp"
	"strconv"
)

var (
	errNotImage = failure(nameData, "graphics: the bytes are not an image")
	scaleSuffix = regexp.MustCompile(`@(\d+(?:\.\d+)?)x\.[^.]+$`)
)

type imageHeader struct {
	width   int
	height  int
	density float64
}

type encodedSource struct {
	data   []byte
	header imageHeader
}

func decodedNode(data []byte, scale *float64, timestamp float64) (*imageNode, error) {
	header, err := inspectImage(data)
	if err != nil {
		return nil, err
	}
	chosen := 1.0
	switch {
	case scale != nil:
		chosen = *scale
	case header.density >= 1 && header.density <= 16:
		chosen = header.density
	}
	return newImageNode(header.width, header.height, scaling(1/chosen, 1/chosen), math.NaN(), timestamp, &encodedSource{data: data, header: header}), nil
}

func fileScale(path string) *float64 {
	match := scaleSuffix.FindStringSubmatch(filepath.Base(path))
	if match == nil {
		return nil
	}
	scale, err := strconv.ParseFloat(match[1], 64)
	if err != nil || !(scale > 0) {
		return nil
	}
	return &scale
}
