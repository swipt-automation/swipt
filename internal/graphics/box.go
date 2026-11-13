//go:build cgo

package graphics

import (
	"bytes"
	"encoding/binary"
)

func pack(values ...any) []byte {
	var out []byte
	for _, value := range values {
		switch typed := value.(type) {
		case uint8:
			out = append(out, typed)
		case uint16:
			out = binary.BigEndian.AppendUint16(out, typed)
		case uint32:
			out = binary.BigEndian.AppendUint32(out, typed)
		case int32:
			out = binary.BigEndian.AppendUint32(out, uint32(typed))
		case uint64:
			out = binary.BigEndian.AppendUint64(out, typed)
		case string:
			out = append(out, typed...)
		case []byte:
			out = append(out, typed...)
		default:
			panic("graphics: pack an unknown value")
		}
	}
	return out
}

func box(kind string, contents ...[]byte) []byte {
	size := 8
	for _, content := range contents {
		size += len(content)
	}
	return pack(uint32(size), kind, bytes.Join(contents, nil))
}

func matrix(a, b, c, d int32) []byte {
	return pack(a<<16, b<<16, uint32(0), c<<16, d<<16, uint32(0), uint32(0), uint32(0), uint32(1<<30))
}
