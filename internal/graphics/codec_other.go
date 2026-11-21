//go:build cgo && !darwin && !android

package graphics

import (
	"bytes"
	"context"
	"encoding/binary"
)

var undecodableFormats = map[string]string{"\xff\xd8\xff": "jpeg", "GIF8": "gif", "BM": "bmp", "II*\x00": "tiff", "MM\x00*": "tiff"}

func inspectImage(data []byte) (imageHeader, error) {
	if !bytes.HasPrefix(data, pngSignature) {
		for prefix, format := range undecodableFormats {
			if bytes.HasPrefix(data, []byte(prefix)) {
				return imageHeader{}, unsupported("decoding " + format)
			}
		}
		if len(data) >= 12 && (string(data[4:8]) == "ftyp" || string(data[8:12]) == "WEBP") {
			return imageHeader{}, unsupported("decoding this format")
		}
		return imageHeader{}, errNotImage
	}
	var header imageHeader
	pngChunks(data, func(kind string, chunk []byte) bool {
		if kind == "IHDR" && len(chunk) >= 20 {
			header.width = int(binary.BigEndian.Uint32(chunk[8:]))
			header.height = int(binary.BigEndian.Uint32(chunk[12:]))
		}
		return false
	})
	if header.width <= 0 || header.height <= 0 {
		return imageHeader{}, errNotImage
	}
	header.density = pngDensity(data)
	return header, nil
}

func encodeImage(_ context.Context, node *imageNode, format string, _ *float64) ([]byte, error) {
	if format != "png" {
		return nil, unsupported("encoding " + format)
	}
	return withPNGDensity(node.source.data, node.scale()), nil
}
