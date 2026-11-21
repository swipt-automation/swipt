//go:build cgo && !darwin

package graphics

import (
	"encoding/binary"
	"hash/crc32"
	"math"
)

const (
	metersPerInch = 0.0254
	pointsPerInch = 72
	pngChunkPHYs  = "pHYs"
)

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

func pngChunks(data []byte, visit func(kind string, chunk []byte) bool) bool {
	for at := len(pngSignature); at+12 <= len(data); {
		length := int(binary.BigEndian.Uint32(data[at:]))
		end := at + 12 + length
		if length < 0 || end > len(data) || !visit(string(data[at+4:at+8]), data[at:end]) {
			return end <= len(data)
		}
		at = end
	}
	return true
}

// pngDensity reads the chunks before the image data; ImageIO ignores those after it (macOS 27.2).
func pngDensity(data []byte) float64 {
	var density float64
	pngChunks(data, func(kind string, chunk []byte) bool {
		content := chunk[8 : len(chunk)-4]
		if kind == pngChunkPHYs && len(content) >= 9 && content[8] == 1 {
			// ImageIO rounds the resolution of a PNG to whole dots per inch (sips -g dpiWidth, macOS 27.2.1 26B131).
			density = math.Round(float64(binary.BigEndian.Uint32(content))*metersPerInch) / pointsPerInch
		}
		return kind != "IDAT"
	})
	return density
}

func withPNGDensity(data []byte, scale float64) []byte {
	density := uint32(min(max(math.Round(pointsPerInch*scale/metersPerInch), 1), math.MaxUint32))
	physical := make([]byte, 21)
	binary.BigEndian.PutUint32(physical, 9)
	copy(physical[4:], pngChunkPHYs)
	binary.BigEndian.PutUint32(physical[8:], density)
	binary.BigEndian.PutUint32(physical[12:], density)
	physical[16] = 1
	binary.BigEndian.PutUint32(physical[17:], crc32.ChecksumIEEE(physical[4:17]))
	out := append(make([]byte, 0, len(data)+len(physical)), pngSignature...)
	complete := pngChunks(data, func(kind string, chunk []byte) bool {
		if kind != pngChunkPHYs {
			out = append(out, chunk...)
		}
		if kind == "IHDR" {
			out = append(out, physical...)
		}
		return true
	})
	if !complete {
		return data
	}
	return out
}
