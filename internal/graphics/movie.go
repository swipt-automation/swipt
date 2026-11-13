//go:build cgo

package graphics

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/rfb/media"
)

const (
	movieTimescale    = 1000
	fragmentDuration  = media.VideoClockRate
	lastFrameDuration = time.Second / 60
	keyframeFlags     = 0x02000000
	dependentFlags    = 0x01010000
)

func fileType(mp4 bool) []byte {
	if mp4 {
		return box("ftyp", pack("isom", uint32(0x200), "isom"))
	}
	return box("ftyp", pack("qt  ", uint32(0), "qt  "))
}

func movieHeader(format *hevcFormat, turns int, duration int64) []byte {
	movieDuration := uint32(duration * movieTimescale / media.VideoClockRate)
	cosine := [4]int32{1, 0, -1, 0}[turns&3]
	sine := [4]int32{0, 1, 0, -1}[turns&3]
	sampleEntry := box("hvc1", pack(
		make([]byte, 6), uint16(1),
		make([]byte, 16), uint16(format.width), uint16(format.height),
		uint32(0x00480000), uint32(0x00480000), uint32(0), uint16(1), make([]byte, 32), uint16(0x0018), uint16(0xffff),
		box("hvcC", format.configuration)))
	return bytes.Join([][]byte{
		fileType(false),
		box("moov",
			box("mvhd", pack(uint32(0), uint32(0), uint32(0), uint32(movieTimescale), movieDuration, uint32(0x00010000), uint16(0x0100), make([]byte, 10),
				matrix(1, 0, 0, 1), make([]byte, 24), uint32(2))),
			box("trak",
				box("tkhd", pack(uint32(3), uint32(0), uint32(0), uint32(1), uint32(0), movieDuration, make([]byte, 8), uint16(0), uint16(0), uint16(0), uint16(0),
					matrix(cosine, sine, -sine, cosine), uint32(format.width)<<16, uint32(format.height)<<16)),
				box("mdia",
					box("mdhd", pack(uint32(0), uint32(0), uint32(0), uint32(media.VideoClockRate), uint32(0), uint16(0x55c4), uint16(0))),
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
	base        int64
	pending     *media.AccessUnit
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

func newMovieWriter(path string, format *hevcFormat, first *media.AccessUnit) (*movieWriter, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fileFailure(err, "graphics: create ", filepath.Base(path))
	}
	writer := &movieWriter{file: file, name: filepath.Base(path), format: format, turns: first.Turns, base: first.Time}
	if _, err = file.Write(movieHeader(format, first.Turns, 0)); err != nil {
		file.Close()
		return nil, fileFailure(err, "graphics: write ", writer.name)
	}
	return writer, nil
}

func (w *movieWriter) append(unit *media.AccessUnit) {
	at := unit.Time - w.base
	if w.pending != nil {
		w.write(w.pending, w.pendingTime, at-w.pendingTime)
	}
	w.pending = unit
	w.pendingTime = at
}

func (w *movieWriter) write(unit *media.AccessUnit, at int64, duration int64) {
	payload := unit.Sample()
	if len(payload) == 0 || w.failure != nil {
		return
	}
	switch {
	case len(w.samples) == 0:
		w.start = max(at, w.end)
	case at > w.end:
		w.samples[len(w.samples)-1].duration += uint32(at - w.end)
	}
	flags := uint32(dependentFlags)
	if unit.Keyframe() {
		flags = keyframeFlags
	}
	w.samples = append(w.samples, movieSample{duration: uint32(max(duration, 0)), size: uint32(len(payload)), flags: flags})
	w.data = append(w.data, payload...)
	w.end = max(at, w.end) + max(duration, 0)
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
		w.write(w.pending, w.pendingTime, int64(lastFrameDuration*media.VideoClockRate/time.Second))
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
