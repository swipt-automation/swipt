package dtx

import (
	"encoding/binary"
	"math"
	"strconv"
	"time"
	"unicode/utf16"

	E "github.com/sagernet/sing/common/exceptions"

	"howett.net/plist"
)

var plistEpoch = time.Date(2001, time.January, 1, 0, 0, 0, 0, time.UTC)

type plistDecoder struct {
	data    []byte
	table   uint64
	refSize uint64
	offsets []uint64
	values  []any
	decoded []bool
	active  []bool
}

// The device writes equal objects once and references them from every place
// they appear: a 212 KB tree push of a Wikipedia page expanded past 16 MB when
// every reference was decoded on its own (iPad17,1, iPadOS 27.2 24B115).
func decodeBinaryPlist(data []byte) (any, error) {
	if len(data) < 40 || string(data[:8]) != "bplist00" {
		return nil, E.New("dtx: invalid binary plist header")
	}
	trailer := data[len(data)-32:]
	offsetSize, refSize := uint64(trailer[6]), uint64(trailer[7])
	count := binary.BigEndian.Uint64(trailer[8:])
	root := binary.BigEndian.Uint64(trailer[16:])
	table := binary.BigEndian.Uint64(trailer[24:])
	end := uint64(len(data) - 32)
	if offsetSize < 1 || offsetSize > 8 || refSize < 1 || refSize > 8 || count == 0 || root >= count ||
		table < 8 || table > end || count > (end-table)/offsetSize {
		return nil, E.New("dtx: invalid binary plist trailer")
	}
	d := &plistDecoder{
		data: data, table: table, refSize: refSize,
		offsets: make([]uint64, count), values: make([]any, count), decoded: make([]bool, count), active: make([]bool, count),
	}
	for index := range d.offsets {
		start := table + uint64(index)*offsetSize
		d.offsets[index] = readBigEndian(data[start : start+offsetSize])
		if d.offsets[index] < 8 || d.offsets[index] >= table {
			return nil, E.New("dtx: invalid binary plist object offset")
		}
	}
	return d.object(root)
}

func readBigEndian(data []byte) uint64 {
	var value uint64
	for _, b := range data {
		value = value<<8 | uint64(b)
	}
	return value
}

func (d *plistDecoder) bytes(start, length uint64) ([]byte, error) {
	if start > d.table || length > d.table-start {
		return nil, E.New("dtx: truncated binary plist object")
	}
	return d.data[start : start+length], nil
}

func (d *plistDecoder) object(index uint64) (any, error) {
	if index >= uint64(len(d.offsets)) {
		return nil, E.New("dtx: binary plist reference ", index, " is out of bounds")
	}
	if d.decoded[index] {
		return d.values[index], nil
	}
	if d.active[index] {
		return nil, E.New("dtx: cyclic binary plist reference ", index)
	}
	d.active[index] = true
	value, err := d.parse(d.offsets[index])
	d.active[index] = false
	if err != nil {
		return nil, err
	}
	d.values[index], d.decoded[index] = value, true
	return value, nil
}

func (d *plistDecoder) parse(offset uint64) (any, error) {
	marker := d.data[offset]
	kind, size := marker>>4, uint64(marker&15)
	start := offset + 1
	if size == 15 && (kind == 4 || kind == 5 || kind == 6 || kind == 10 || kind == 12 || kind == 13) {
		header, err := d.bytes(start, 1)
		if err != nil {
			return nil, err
		}
		if header[0]>>4 != 1 || header[0]&15 > 3 {
			return nil, E.New("dtx: invalid binary plist object length")
		}
		width := uint64(1) << (header[0] & 15)
		length, err := d.bytes(start+1, width)
		if err != nil {
			return nil, err
		}
		size = readBigEndian(length)
		start += 1 + width
	}
	switch kind {
	case 0:
		switch marker {
		case 0:
			return nil, nil
		case 8:
			return false, nil
		case 9:
			return true, nil
		}
	case 1:
		if size > 4 {
			break
		}
		data, err := d.bytes(start, 1<<size)
		if err != nil {
			return nil, err
		}
		if size == 4 {
			high, low := binary.BigEndian.Uint64(data), binary.BigEndian.Uint64(data[8:])
			if high == math.MaxUint64 {
				return int64(low), nil
			}
			return low, nil
		}
		return readBigEndian(data), nil
	case 2:
		if size != 2 && size != 3 {
			break
		}
		data, err := d.bytes(start, 1<<size)
		if err != nil {
			return nil, err
		}
		if size == 2 {
			return float64(math.Float32frombits(binary.BigEndian.Uint32(data))), nil
		}
		return math.Float64frombits(binary.BigEndian.Uint64(data)), nil
	case 3:
		if size != 3 {
			break
		}
		data, err := d.bytes(start, 8)
		if err != nil {
			return nil, err
		}
		seconds := math.Float64frombits(binary.BigEndian.Uint64(data))
		return plistEpoch.Add(time.Duration(seconds * float64(time.Second))), nil
	case 4:
		return d.bytes(start, size)
	case 5:
		data, err := d.bytes(start, size)
		if err != nil {
			return nil, err
		}
		return string(data), nil
	case 6:
		if size > math.MaxUint64/2 {
			break
		}
		data, err := d.bytes(start, size*2)
		if err != nil {
			return nil, err
		}
		units := make([]uint16, size)
		for index := range units {
			units[index] = binary.BigEndian.Uint16(data[index*2:])
		}
		return string(utf16.Decode(units)), nil
	case 8:
		data, err := d.bytes(start, size+1)
		if err != nil {
			return nil, err
		}
		return plist.UID(readBigEndian(data)), nil
	case 10, 12:
		refs, err := d.refs(start, size)
		if err != nil {
			return nil, err
		}
		values := make([]any, len(refs))
		for index, ref := range refs {
			values[index], err = d.object(ref)
			if err != nil {
				return nil, err
			}
		}
		return values, nil
	case 13:
		if size > math.MaxUint64/2 {
			break
		}
		refs, err := d.refs(start, size*2)
		if err != nil {
			return nil, err
		}
		values := make(map[string]any, size)
		for index := range size {
			key, err := d.object(refs[index])
			if err != nil {
				return nil, err
			}
			name, ok := key.(string)
			if !ok {
				return nil, E.New("dtx: binary plist dictionary key is not a string")
			}
			values[name], err = d.object(refs[size+index])
			if err != nil {
				return nil, err
			}
		}
		return values, nil
	}
	return nil, E.New("dtx: unsupported binary plist object 0x", strconv.FormatUint(uint64(marker), 16))
}

func (d *plistDecoder) refs(start, count uint64) ([]uint64, error) {
	if count > d.table/d.refSize {
		return nil, E.New("dtx: truncated binary plist container")
	}
	data, err := d.bytes(start, count*d.refSize)
	if err != nil {
		return nil, err
	}
	refs := make([]uint64, count)
	for index := range refs {
		refs[index] = readBigEndian(data[uint64(index)*d.refSize : uint64(index+1)*d.refSize])
	}
	return refs, nil
}
