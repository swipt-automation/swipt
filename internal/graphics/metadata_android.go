//go:build android && cgo

package graphics

import (
	"bytes"
	"encoding/binary"
	"math"
)

var (
	exifPrefix   = []byte("Exif\x00\x00")
	heifExifType = uint64(binary.BigEndian.Uint32([]byte("Exif")))
)

func densityOf(data []byte) float64 {
	var density float64
	switch {
	case bytes.HasPrefix(data, pngSignature):
		// The resolution in EXIF outranks the pHYs chunk in ImageIO (macOS 27.2).
		if density = pngExifDensity(data); density == 0 {
			density = pngDensity(data)
		}
	case bytes.HasPrefix(data, []byte{0xff, 0xd8}):
		density = jpegDensity(data)
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		density = webpDensity(data)
	case bytes.HasPrefix(data, []byte("II*\x00")) || bytes.HasPrefix(data, []byte("MM\x00*")):
		if directory, readable := readTIFF(data); readable {
			density = directory.density()
		}
	case len(data) >= 38 && string(data[:2]) == "BM" && binary.LittleEndian.Uint32(data[14:]) >= 40:
		// ImageIO keeps the resolution of a BMP unrounded (macOS 27.2).
		density = float64(int32(binary.LittleEndian.Uint32(data[38:]))) * metersPerInch / pointsPerInch
	case isHEIF(data):
		density = heifDensity(data)
	}
	return max(density, 0)
}

func pngExifDensity(data []byte) float64 {
	var density float64
	pngChunks(data, func(kind string, chunk []byte) bool {
		if kind != "eXIf" {
			return kind != "IDAT"
		}
		directory, readable := readTIFF(chunk[8 : len(chunk)-4])
		if readable {
			density = directory.density()
		}
		return !readable
	})
	return density
}

func jpegDensity(data []byte) float64 {
	var jfif, exif float64
	var exifRead bool
	for at := 2; at+4 <= len(data) && data[at] == 0xff; {
		marker := data[at+1]
		if marker == 0xda || marker == 0xd9 {
			break
		}
		length := int(binary.BigEndian.Uint16(data[at+2:]))
		end := at + 2 + length
		if length < 2 || end > len(data) {
			break
		}
		segment := data[at+4 : end]
		switch {
		case marker == 0xe0 && len(segment) >= 12 && string(segment[:5]) == "JFIF\x00":
			density := float64(binary.BigEndian.Uint16(segment[8:]))
			switch segment[7] {
			case 1:
				jfif = density / pointsPerInch
			case 2:
				jfif = density * 2.54 / pointsPerInch
			}
		case marker == 0xe1 && !exifRead && bytes.HasPrefix(segment, exifPrefix):
			// ImageIO reads the first EXIF segment (macOS 27.2).
			if directory, readable := readTIFF(segment[len(exifPrefix):]); readable {
				exifRead = true
				exif = directory.density()
			}
		}
		at = end
	}
	// The resolution in EXIF outranks JFIF in ImageIO (macOS 27.2).
	if exif == 0 {
		return jfif
	}
	return exif
}

func webpDensity(data []byte) float64 {
	var density float64
	for at := 12; at+8 <= len(data); {
		length := int(binary.LittleEndian.Uint32(data[at+4:]))
		end := at + 8 + length
		if length < 0 || end > len(data) {
			break
		}
		if string(data[at:at+4]) == "EXIF" {
			if directory, readable := readTIFF(bytes.TrimPrefix(data[at+8:end], exifPrefix)); readable && directory.density() > 0 {
				density = directory.density()
			}
		}
		at = end + length&1
	}
	return density
}

const (
	tiffTagXResolution    = 0x011a
	tiffTagResolutionUnit = 0x0128

	tiffTypeShort    = 3
	tiffTypeRational = 5
)

type tiffDirectory struct {
	data    []byte
	order   binary.ByteOrder
	entries []byte
}

func readTIFF(data []byte) (tiffDirectory, bool) {
	if len(data) < 8 {
		return tiffDirectory{}, false
	}
	var order binary.ByteOrder
	switch string(data[:4]) {
	case "II*\x00":
		order = binary.LittleEndian
	case "MM\x00*":
		order = binary.BigEndian
	default:
		return tiffDirectory{}, false
	}
	at := int64(order.Uint32(data[4:]))
	if at+2 > int64(len(data)) {
		return tiffDirectory{}, false
	}
	end := min(at+2+int64(order.Uint16(data[at:]))*12, int64(len(data)))
	return tiffDirectory{data: data, order: order, entries: data[at+2 : end]}, true
}

var tiffTypeSizes = map[uint16]int64{tiffTypeShort: 2, tiffTypeRational: 8}

func (d tiffDirectory) field(tag, kind uint16) []byte {
	for at := 0; at+12 <= len(d.entries); at += 12 {
		entry := d.entries[at : at+12]
		if d.order.Uint16(entry) != tag {
			continue
		}
		if d.order.Uint16(entry[2:]) != kind {
			return nil
		}
		size := int64(d.order.Uint32(entry[4:])) * tiffTypeSizes[kind]
		if size <= 4 {
			return entry[8 : 8+size]
		}
		offset := int64(d.order.Uint32(entry[8:]))
		if offset+size > int64(len(d.data)) {
			return nil
		}
		return d.data[offset : offset+size]
	}
	return nil
}

func (d tiffDirectory) short(tag uint16) uint16 {
	value := d.field(tag, tiffTypeShort)
	if len(value) < 2 {
		return 0
	}
	return d.order.Uint16(value)
}

func (d tiffDirectory) rational(tag uint16) float64 {
	value := d.field(tag, tiffTypeRational)
	if len(value) < 8 || d.order.Uint32(value[4:]) == 0 {
		return 0
	}
	return float64(d.order.Uint32(value)) / float64(d.order.Uint32(value[4:]))
}

// density reads the resolution the way ImageIO does (macOS 27.2): a resolution in centimeters is rounded to whole dots
// per inch.
func (d tiffDirectory) density() float64 {
	resolution := d.rational(tiffTagXResolution)
	if d.short(tiffTagResolutionUnit) == 3 {
		resolution = math.Round(resolution * 2.54)
	}
	return max(resolution, 0) / pointsPerInch
}

func isHEIF(data []byte) bool {
	fileType, found := isoBox(data, "ftyp")
	if !found || len(data) < 8 || string(data[4:8]) != "ftyp" {
		return false
	}
	for at := 0; at+4 <= len(fileType); at += 4 {
		if at == 4 {
			continue
		}
		switch string(fileType[at : at+4]) {
		case "mif1", "msf1", "heic", "heix", "heim", "heis", "hevc", "hevx", "avif", "avis":
			return true
		}
	}
	return false
}

func heifDensity(data []byte) float64 {
	meta, found := isoBox(data, "meta")
	if !found || len(meta) < 4 {
		return 0
	}
	meta = meta[4:]
	item, found := heifExifItem(meta)
	if !found {
		return 0
	}
	exif := heifItem(data, meta, item)
	if len(exif) < 4 || uint64(binary.BigEndian.Uint32(exif))+4 > uint64(len(exif)) {
		return 0
	}
	directory, readable := readTIFF(exif[4+binary.BigEndian.Uint32(exif):])
	if !readable {
		return 0
	}
	return directory.density()
}

func isoBox(data []byte, kind string) ([]byte, bool) {
	found := false
	var content []byte
	isoBoxes(data, func(boxKind string, boxContent []byte) bool {
		found, content = boxKind == kind, boxContent
		return !found
	})
	return content, found
}

func isoBoxes(data []byte, visit func(kind string, content []byte) bool) {
	for at := uint64(0); at+8 <= uint64(len(data)); {
		size, header := uint64(binary.BigEndian.Uint32(data[at:])), uint64(8)
		switch size {
		case 0:
			size = uint64(len(data)) - at
		case 1:
			if at+16 > uint64(len(data)) {
				return
			}
			size, header = binary.BigEndian.Uint64(data[at+8:]), 16
		}
		if size < header || size > uint64(len(data))-at || !visit(string(data[at+4:at+8]), data[at+header:at+size]) {
			return
		}
		at += size
	}
}

type isoReader struct {
	data   []byte
	failed bool
}

func (r *isoReader) uint(size int) uint64 {
	if r.failed || size > len(r.data) {
		r.failed = true
		return 0
	}
	var value uint64
	for _, digit := range r.data[:size] {
		value = value<<8 | uint64(digit)
	}
	r.data = r.data[size:]
	return value
}

func heifExifItem(meta []byte) (uint32, bool) {
	pitm, found := isoBox(meta, "pitm")
	if !found {
		return 0, false
	}
	reader := isoReader{data: pitm}
	idSize := 2
	if reader.uint(4) != 0 {
		idSize = 4
	}
	primary := reader.uint(idSize)
	exif := make(map[uint64]bool)
	if iinf, found := isoBox(meta, "iinf"); found {
		reader = isoReader{data: iinf}
		countSize := 2
		if reader.uint(4) != 0 {
			countSize = 4
		}
		reader.uint(countSize)
		isoBoxes(reader.data, func(kind string, entry []byte) bool {
			reader := isoReader{data: entry}
			switch version := reader.uint(4) >> 24; {
			case kind != "infe" || version < 2:
			case version == 2:
				item, _, itemKind := reader.uint(2), reader.uint(2), reader.uint(4)
				exif[item] = itemKind == heifExifType && !reader.failed
			default:
				item, _, itemKind := reader.uint(4), reader.uint(2), reader.uint(4)
				exif[item] = itemKind == heifExifType && !reader.failed
			}
			return true
		})
	}
	iref, found := isoBox(meta, "iref")
	if reader.failed || !found {
		return 0, false
	}
	reader = isoReader{data: iref}
	idSize = 2
	if reader.uint(4) != 0 {
		idSize = 4
	}
	var described uint64
	isoBoxes(reader.data, func(kind string, entry []byte) bool {
		reader := isoReader{data: entry}
		from, count := reader.uint(idSize), reader.uint(2)
		for range count {
			if to := reader.uint(idSize); kind == "cdsc" && to == primary && exif[from] && !reader.failed {
				described = from
				return false
			}
		}
		return true
	})
	return uint32(described), described != 0
}

func heifItem(data, meta []byte, item uint32) []byte {
	iloc, found := isoBox(meta, "iloc")
	if !found {
		return nil
	}
	idat, _ := isoBox(meta, "idat")
	reader := isoReader{data: iloc}
	version := reader.uint(4) >> 24
	sizes := reader.uint(2)
	offsetSize, lengthSize, baseSize, indexSize := int(sizes>>12), int(sizes>>8&15), int(sizes>>4&15), 0
	idSize := 2
	if version >= 1 {
		indexSize = int(sizes & 15)
	}
	if version == 2 {
		idSize = 4
	}
	for range reader.uint(idSize) {
		id, method := reader.uint(idSize), uint64(0)
		if version >= 1 {
			method = reader.uint(2) & 15
		}
		reader.uint(2)
		base := reader.uint(baseSize)
		var content []byte
		for range reader.uint(2) {
			reader.uint(indexSize)
			offset, length := base+reader.uint(offsetSize), reader.uint(lengthSize)
			source := map[uint64][]byte{0: data, 1: idat}[method]
			if length == 0 && offset <= uint64(len(source)) {
				length = uint64(len(source)) - offset
			}
			if id == uint64(item) && offset <= uint64(len(source)) && length <= uint64(len(source))-offset {
				content = append(content, source[offset:offset+length]...)
			}
		}
		if reader.failed {
			return nil
		}
		if id == uint64(item) {
			return content
		}
	}
	return nil
}
