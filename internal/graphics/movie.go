//go:build cgo

package graphics

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io/fs"
	"math/bits"
	"os"
	"path/filepath"

	E "github.com/sagernet/sing/common/exceptions"
)

const (
	mediaTimescale     = 24000
	movieTimescale     = 1000
	fragmentDuration   = mediaTimescale
	lastSampleDuration = 400
	keyframeFlags      = 0x02000000
	dependentFlags     = 0x01010000
)

type hevcFormat struct {
	vps           []byte
	sps           []byte
	pps           []byte
	width         int
	height        int
	configuration []byte
}

type bitReader struct {
	data     []byte
	at       int
	overflow bool
}

func (r *bitReader) bits(count int) uint64 {
	var value uint64
	for range count {
		if r.at >= len(r.data)*8 {
			r.overflow = true
			return 0
		}
		value = value<<1 | uint64(r.data[r.at/8]>>(7-r.at%8)&1)
		r.at++
	}
	return value
}

func (r *bitReader) exponential() uint64 {
	zeros := 0
	for r.bits(1) == 0 && !r.overflow {
		zeros++
		if zeros > 32 {
			r.overflow = true
			return 0
		}
	}
	return 1<<zeros - 1 + r.bits(zeros)
}

type bitWriter struct {
	data []byte
	at   int
}

func (w *bitWriter) bits(value uint64, count int) {
	for index := count - 1; index >= 0; index-- {
		if w.at%8 == 0 {
			w.data = append(w.data, 0)
		}
		w.data[len(w.data)-1] |= byte(value>>index&1) << (7 - w.at%8)
		w.at++
	}
}

func (w *bitWriter) exponential(value uint64) {
	w.bits(value+1, 2*bits.Len64(value+1)-1)
}

func escape(rbsp []byte) []byte {
	out := make([]byte, 0, len(rbsp))
	zeros := 0
	for _, value := range rbsp {
		if zeros >= 2 && value <= 3 {
			out = append(out, 3)
			zeros = 0
		}
		out = append(out, value)
		if value == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

func unescape(nal []byte) []byte {
	out := make([]byte, 0, len(nal))
	zeros := 0
	for _, value := range nal {
		if zeros >= 2 && value == 3 {
			zeros = 0
			continue
		}
		out = append(out, value)
		if value == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return out
}

func parseHEVCFormat(unit *accessUnit, screenWidth, screenHeight int) (*hevcFormat, error) {
	format := &hevcFormat{vps: unit.nal(nalVPS), sps: unit.nal(nalSPS), pps: unit.nal(nalPPS)}
	if format.vps == nil || format.sps == nil || format.pps == nil {
		return nil, E.New("graphics: the parameter sets are incomplete")
	}
	sps := unescape(format.sps)
	if len(sps) < 15 {
		return nil, E.New("graphics: the sequence parameter set is short")
	}
	reader := bitReader{data: sps, at: 16}
	reader.bits(4)
	subLayers := int(reader.bits(3))
	nesting := reader.bits(1)
	reader.bits(96)
	present := make([][2]bool, subLayers)
	for index := range present {
		present[index] = [2]bool{reader.bits(1) == 1, reader.bits(1) == 1}
	}
	if subLayers > 0 {
		reader.bits(2 * (8 - subLayers))
	}
	for _, flags := range present {
		if flags[0] {
			reader.bits(88)
		}
		if flags[1] {
			reader.bits(8)
		}
	}
	reader.exponential()
	chroma := reader.exponential()
	separate := chroma == 3 && reader.bits(1) == 1
	width, height := reader.exponential(), reader.exponential()
	window := reader.at
	var left, right, top, bottom uint64
	if reader.bits(1) == 1 {
		left, right, top, bottom = reader.exponential(), reader.exponential(), reader.exponential(), reader.exponential()
	}
	rest := reader.at
	lumaDepth, chromaDepth := reader.exponential(), reader.exponential()
	if reader.overflow || chroma > 3 || lumaDepth > 7 || chromaDepth > 7 {
		return nil, E.New("graphics: read the sequence parameter set")
	}
	subWidth, subHeight := uint64(1), uint64(1)
	if !separate && (chroma == 1 || chroma == 2) {
		subWidth = 2
	}
	if !separate && chroma == 1 {
		subHeight = 2
	}
	format.width = int(width - subWidth*(left+right))
	format.height = int(height - subHeight*(top+bottom))
	if format.width <= 0 || format.height <= 0 || format.width > 0xffff || format.height > 0xffff {
		return nil, E.New("graphics: the sequence parameter set has no usable size")
	}
	parameterSets := [][]byte{format.vps, format.sps, format.pps}
	extraWidth := uint64(max(format.width-screenWidth, 0)) / subWidth
	extraHeight := uint64(max(format.height-screenHeight, 0)) / subHeight
	last := bytes.LastIndexFunc(sps, func(value rune) bool { return value != 0 })
	if screenWidth > 0 && screenHeight > 0 && extraWidth+extraHeight > 0 && last*8 >= rest {
		var cropped bitWriter
		copyBits := func(from, to int) {
			for reader.at = from; reader.at < to; {
				cropped.bits(reader.bits(1), 1)
			}
		}
		copyBits(0, window)
		cropped.bits(1, 1)
		for _, value := range []uint64{left, right + extraWidth, top, bottom + extraHeight} {
			cropped.exponential(value)
		}
		copyBits(rest, last*8+8-bits.TrailingZeros8(sps[last]))
		parameterSets[1] = escape(cropped.data)
		format.width -= int(extraWidth * subWidth)
		format.height -= int(extraHeight * subHeight)
	}
	configuration := append([]byte{1}, sps[3:15]...)
	configuration = append(configuration, 0xf0, 0x00, 0xfc, 0xfc|byte(chroma), 0xf8|byte(lumaDepth), 0xf8|byte(chromaDepth), 0, 0,
		byte(min(subLayers+1, 7))<<3|byte(nesting)<<2|3, 3)
	for _, nal := range parameterSets {
		configuration = append(configuration, 0x80|byte(nalType(nal)))
		configuration = binary.BigEndian.AppendUint16(configuration, 1)
		configuration = binary.BigEndian.AppendUint16(configuration, uint16(len(nal)))
		configuration = append(configuration, nal...)
	}
	format.configuration = configuration
	return format, nil
}

func (f *hevcFormat) equal(other *hevcFormat) bool {
	return bytes.Equal(f.vps, other.vps) && bytes.Equal(f.sps, other.sps) && bytes.Equal(f.pps, other.pps)
}

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

func movieHeader(format *hevcFormat, turns int, duration int64) []byte {
	movieDuration := uint32(duration * movieTimescale / mediaTimescale)
	cosine := [4]int32{1, 0, -1, 0}[turns&3]
	sine := [4]int32{0, 1, 0, -1}[turns&3]
	sampleEntry := box("hvc1", pack(
		make([]byte, 6), uint16(1),
		make([]byte, 16), uint16(format.width), uint16(format.height),
		uint32(0x00480000), uint32(0x00480000), uint32(0), uint16(1), make([]byte, 32), uint16(0x0018), uint16(0xffff),
		box("hvcC", format.configuration)))
	return bytes.Join([][]byte{
		box("ftyp", pack("qt  ", uint32(0), "qt  ")),
		box("moov",
			box("mvhd", pack(uint32(0), uint32(0), uint32(0), uint32(movieTimescale), movieDuration, uint32(0x00010000), uint16(0x0100), make([]byte, 10),
				matrix(1, 0, 0, 1), make([]byte, 24), uint32(2))),
			box("trak",
				box("tkhd", pack(uint32(3), uint32(0), uint32(0), uint32(1), uint32(0), movieDuration, make([]byte, 8), uint16(0), uint16(0), uint16(0), uint16(0),
					matrix(cosine, sine, -sine, cosine), uint32(format.width)<<16, uint32(format.height)<<16)),
				box("mdia",
					box("mdhd", pack(uint32(0), uint32(0), uint32(0), uint32(mediaTimescale), uint32(0), uint16(0x55c4), uint16(0))),
					box("hdlr", pack(uint32(0), "mhlr", "vide", make([]byte, 12), uint8(0))),
					box("minf",
						box("vmhd", pack(uint32(1), uint16(0), make([]byte, 6))),
						box("dinf", box("dref", pack(uint32(0), uint32(1)), box("url ", pack(uint32(1))))),
						box("stbl",
							box("stsd", pack(uint32(0), uint32(1)), sampleEntry),
							box("stts", pack(uint32(0), uint32(0))),
							box("stsc", pack(uint32(0), uint32(0))),
							box("stsz", pack(uint32(0), uint32(0), uint32(0))),
							box("stco", pack(uint32(0), uint32(0))))))),
			box("mvex",
				box("mehd", pack(uint32(0), movieDuration)),
				box("trex", pack(uint32(0), uint32(1), uint32(1), uint32(0), uint32(0), uint32(0))))),
	}, nil)
}

type rtpClock struct {
	hasLast bool
	last    uint32
	high    int64
	hasBase bool
	base    int64
}

func (c *rtpClock) time(timestamp uint32) int64 {
	if c.hasLast && timestamp < c.last && c.last-timestamp > 0x80000000 {
		c.high += 1 << 32
	}
	c.hasLast = true
	c.last = timestamp
	value := c.high + int64(timestamp)
	if !c.hasBase {
		c.hasBase = true
		c.base = value
	}
	return value - c.base
}

type movieSample struct {
	duration uint32
	size     uint32
	flags    uint32
}

type movieWriter struct {
	file        *os.File
	name        string
	format      *hevcFormat
	turns       int
	clock       rtpClock
	pending     *accessUnit
	pendingTime int64
	samples     []movieSample
	data        []byte
	start       int64
	end         int64
	sequence    uint32
	failure     error
}

func fileFailure(err error, message ...any) error {
	if pathError, isPath := errors.AsType[*fs.PathError](err); isPath {
		err = pathError.Err
	}
	return E.Cause(err, message...)
}

func newMovieWriter(path string, format *hevcFormat, turns int) (*movieWriter, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fileFailure(err, "graphics: create ", filepath.Base(path))
	}
	writer := &movieWriter{file: file, name: filepath.Base(path), format: format, turns: turns}
	if _, err = file.Write(movieHeader(format, turns, 0)); err != nil {
		file.Close()
		return nil, fileFailure(err, "graphics: write ", writer.name)
	}
	return writer, nil
}

func (w *movieWriter) append(unit *accessUnit) {
	time := w.clock.time(unit.rtpTimestamp)
	if w.pending != nil {
		w.write(w.pending, w.pendingTime, time-w.pendingTime)
	}
	w.pending = unit
	w.pendingTime = time
}

func (w *movieWriter) write(unit *accessUnit, time int64, duration int64) {
	payload := unit.sample()
	if len(payload) == 0 || w.failure != nil {
		return
	}
	switch {
	case len(w.samples) == 0:
		w.start = max(time, w.end)
	case time > w.end:
		w.samples[len(w.samples)-1].duration += uint32(time - w.end)
	}
	flags := uint32(dependentFlags)
	if unit.keyframe() {
		flags = keyframeFlags
	}
	w.samples = append(w.samples, movieSample{duration: uint32(max(duration, 0)), size: uint32(len(payload)), flags: flags})
	w.data = append(w.data, payload...)
	w.end = max(time, w.end) + max(duration, 0)
	if w.end-w.start >= fragmentDuration {
		w.flush()
	}
}

func (w *movieWriter) fragment(offset int32) []byte {
	run := pack(uint32(0x000701), uint32(len(w.samples)), offset)
	for _, sample := range w.samples {
		run = append(run, pack(sample.duration, sample.size, sample.flags)...)
	}
	return box("moof",
		box("mfhd", pack(uint32(0), w.sequence)),
		box("traf",
			box("tfhd", pack(uint32(0x020000), uint32(1))),
			box("tfdt", pack(uint32(0x01000000), uint64(w.start))),
			box("trun", run)))
}

func (w *movieWriter) flush() {
	if len(w.samples) == 0 || w.failure != nil {
		return
	}
	w.sequence++
	fragment := w.fragment(int32(len(w.fragment(0)) + 8))
	fragment = append(fragment, pack(uint32(8+len(w.data)), "mdat")...)
	if _, err := w.file.Write(append(fragment, w.data...)); err != nil {
		w.failure = fileFailure(err, "graphics: write ", w.name)
	}
	w.samples = w.samples[:0]
	w.data = w.data[:0]
}

func (w *movieWriter) finish() error {
	if w.pending != nil {
		w.write(w.pending, w.pendingTime, lastSampleDuration)
		w.pending = nil
	}
	w.flush()
	if w.failure == nil {
		if _, err := w.file.WriteAt(movieHeader(w.format, w.turns, w.end), 0); err != nil {
			w.failure = fileFailure(err, "graphics: write ", w.name)
		}
	}
	if err := w.file.Close(); err != nil && w.failure == nil {
		w.failure = fileFailure(err, "graphics: finish ", w.name)
	}
	return w.failure
}
