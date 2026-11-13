package media

import "math/bits"

func unescapeRBSP(data []byte) []byte {
	output := make([]byte, 0, len(data))
	zeros := 0
	for _, value := range data {
		if zeros >= 2 && value == 3 {
			zeros = 0
			continue
		}
		output = append(output, value)
		if value == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return output
}

func escapeRBSP(data []byte) []byte {
	output := make([]byte, 0, len(data)+len(data)/64)
	zeros := 0
	for _, value := range data {
		if zeros >= 2 && value <= 3 {
			output = append(output, 3)
			zeros = 0
		}
		output = append(output, value)
		if value == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return output
}

type bitReader struct {
	data     []byte
	position int
	overrun  bool
}

func (r *bitReader) bit() uint {
	if r.position >= len(r.data)*8 {
		r.overrun = true
		return 0
	}
	value := uint(r.data[r.position/8]>>(7-r.position%8)) & 1
	r.position++
	return value
}

func (r *bitReader) bits(count int) uint {
	var value uint
	for range count {
		value = value<<1 | r.bit()
	}
	return value
}

func (r *bitReader) flag() bool {
	return r.bit() == 1
}

func (r *bitReader) unsignedGolomb() uint {
	zeros := 0
	for r.bit() == 0 {
		if r.overrun || zeros > 31 {
			r.overrun = true
			return 0
		}
		zeros++
	}
	return 1<<zeros - 1 + r.bits(zeros)
}

type bitWriter struct {
	data     []byte
	position int
}

func (w *bitWriter) bits(value uint, count int) {
	for index := count - 1; index >= 0; index-- {
		if w.position%8 == 0 {
			w.data = append(w.data, 0)
		}
		w.data[len(w.data)-1] |= byte(value>>index&1) << (7 - w.position%8)
		w.position++
	}
}

func (w *bitWriter) unsignedGolomb(value uint) {
	w.bits(value+1, 2*bits.Len(value+1)-1)
}

func (w *bitWriter) copy(r *bitReader, end int) {
	for r.position < end {
		w.bits(r.bit(), 1)
	}
}
