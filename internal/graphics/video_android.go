//go:build android && cgo

package graphics

/*
#include "graphics_android.h"
*/
import "C"

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"runtime/cgo"
	"sync"
	"time"

	"github.com/swipt-automation/swipt/internal/rfb/media"
	"github.com/swipt-automation/swipt/internal/screen"
)

const (
	movieOutputWait = 10 * time.Millisecond
	movieReuseSpan  = 2 * time.Second
	frameTolerance  = 500 * time.Microsecond
)

func microseconds(duration time.Duration) C.int64_t {
	return C.int64_t(duration / time.Microsecond)
}

type videoFrame struct {
	width  int
	height int
	time   int64

	access     sync.Mutex
	references int
	native     *C.swipt_video_frame
}

func newVideoFrame(native *C.swipt_video_frame) *videoFrame {
	var width, height C.int32_t
	C.swipt_video_frame_size(native, &width, &height)
	return &videoFrame{width: int(width), height: int(height), time: int64(C.swipt_video_frame_time(native)), references: 1, native: native}
}

func (f *videoFrame) size() (int, int) {
	return f.width, f.height
}

func (f *videoFrame) raster(width, height int) (*raster, error) {
	f.access.Lock()
	defer f.access.Unlock()
	if f.native == nil {
		return nil, errImageReleased
	}
	out := newRaster(width, height, 4)
	C.swipt_video_frame_convert(f.native, (*C.uint8_t)(&out.pixels[0]), C.int32_t(width), C.int32_t(height), C.size_t(out.stride))
	return out, nil
}

func (f *videoFrame) detach() {
	f.access.Lock()
	defer f.access.Unlock()
	if f.native != nil {
		C.swipt_video_frame_detach(f.native)
	}
}

func (f *videoFrame) retain() *videoFrame {
	f.access.Lock()
	defer f.access.Unlock()
	f.references++
	return f
}

func (f *videoFrame) release() {
	f.access.Lock()
	defer f.access.Unlock()
	f.references--
	if f.references == 0 && f.native != nil {
		C.swipt_video_frame_release(f.native)
		f.native = nil
	}
}

type drawnStamp struct {
	arrival float64
	turns   int
}

type drawnFrames struct {
	stream   *screenStream
	local    screen.LocalStream
	receiver cgo.Handle
	native   *C.swipt_frames
	window   uintptr

	access sync.Mutex
	stamps map[int64]drawnStamp
}

func newLiveFrames(stream *screenStream) (liveFrames, error) {
	local := stream.device.(screen.LocalStream)
	f := &drawnFrames{stream: stream, local: local, stamps: make(map[int64]drawnStamp)}
	f.receiver = cgo.NewHandle(f)
	var window C.uintptr_t
	if message := C.swipt_screen_frames_create(C.int32_t(stream.info.Width), C.int32_t(stream.info.Height), C.uintptr_t(f.receiver), &f.native, &window); message != nil {
		f.receiver.Delete()
		return nil, failed(nameUnavailable, message)
	}
	f.window = uintptr(window)
	local.Draw(f.window, f.drawn, f.failed)
	return f, nil
}

func (f *drawnFrames) failed(err error) {
	s := f.stream
	s.decoding.Lock()
	defer s.decoding.Unlock()
	if s.frames != f {
		return
	}
	s.access.Lock()
	s.undecodable = failure(nameUnavailable, "graphics: the screen stopped drawing its frames: ", err)
	latest := s.takeLatestLocked()
	s.access.Unlock()
	if latest != nil {
		latest.release()
	}
}

func (f *drawnFrames) drawn(nanoseconds int64, turns int) {
	f.access.Lock()
	defer f.access.Unlock()
	f.stamps[nanoseconds/int64(time.Microsecond)] = drawnStamp{arrival: milliseconds(time.Now()), turns: turns}
}

func (f *drawnFrames) unit(*media.AccessUnit, *hevcFormat, bool, bool) bool {
	return false
}

func (f *drawnFrames) close() {
	f.local.StopDrawing(f.window)
	C.swipt_screen_frames_close(f.native)
	f.receiver.Delete()
}

//export swiptGraphicsScreenFrame
func swiptGraphicsScreenFrame(receiver C.uintptr_t, native *C.swipt_video_frame) {
	f := cgo.Handle(receiver).Value().(*drawnFrames)
	frame := newVideoFrame(native)
	f.access.Lock()
	stamp, known := f.stamps[frame.time]
	for at := range f.stamps {
		if at <= frame.time {
			delete(f.stamps, at)
		}
	}
	f.access.Unlock()
	if !known {
		frame.release()
		return
	}
	f.stream.deliver(frameNode(frame, f.stream.info, stamp.turns, math.NaN(), stamp.arrival))
}

type movieFile struct {
	file   *os.File
	native *C.swipt_movie

	access  sync.Mutex
	current *videoFrame
	pending *videoFrame
	ended   bool
}

func openMovie(path string) (movieReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fileFailure(err, "graphics: read ", filepath.Base(path))
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, fileFailure(err, "graphics: read ", filepath.Base(path))
	}
	var native *C.swipt_movie
	if message := C.swipt_movie_open(C.int(file.Fd()), C.int64_t(info.Size()), &native); message != nil {
		file.Close()
		return nil, failed(nameData, message)
	}
	return &movieFile{file: file, native: native}, nil
}

func (m *movieFile) show(frame *videoFrame) {
	if m.current != nil {
		m.current.release()
	}
	m.current = frame
}

func (m *movieFile) forget() {
	m.show(nil)
	if m.pending != nil {
		m.pending.release()
		m.pending = nil
	}
}

func (m *movieFile) frame(ctx context.Context, seconds float64) (nativeFrame, error) {
	m.access.Lock()
	defer m.access.Unlock()
	target := int64(math.Round(seconds * float64(time.Second/time.Microsecond)))
	latest := target + int64(frameTolerance/time.Microsecond)
	if m.current == nil || m.current.time > target || target-m.current.time >= int64(movieReuseSpan/time.Microsecond) {
		m.forget()
		m.ended = false
		if message := C.swipt_movie_seek(m.native, C.int64_t(target)); message != nil {
			return nil, failed(nameGeneric, message)
		}
	}
	if m.pending != nil && m.pending.time <= latest {
		m.show(m.pending)
		m.pending = nil
	}
	for m.pending == nil && !m.ended {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var native *C.swipt_video_frame
		var message *C.char
		switch C.swipt_movie_next(m.native, microseconds(movieOutputWait), &native, &message) {
		case C.SWIPT_VIDEO_DONE:
			if frame := newVideoFrame(native); frame.time <= latest {
				m.show(frame)
			} else {
				m.pending = frame
			}
		case C.SWIPT_VIDEO_ENDED:
			m.ended = true
		case C.SWIPT_VIDEO_FAILED:
			m.forget()
			return nil, failed(nameGeneric, message)
		}
	}
	shown := m.current
	if shown == nil {
		shown = m.pending
	}
	if shown == nil {
		return nil, failure(nameData, "graphics: the video has no frame at ", int(seconds*1000), " ms")
	}
	return shown.retain(), nil
}

func (m *movieFile) close() {
	m.access.Lock()
	defer m.access.Unlock()
	m.forget()
	C.swipt_movie_close(m.native)
	m.file.Close()
}

type videoEncoder struct {
	ctx    context.Context
	path   string
	file   *os.File
	native *C.swipt_encoder
}

func newMovieEncoder(ctx context.Context, path string, mp4 bool, width, height int) (movieEncoder, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fileFailure(err, "graphics: create ", filepath.Base(path))
	}
	var native *C.swipt_encoder
	if message := C.swipt_encoder_create(C.int(file.Fd()), C.bool(mp4), C.int32_t(width), C.int32_t(height), exportFrameRate, &native); message != nil {
		file.Close()
		os.Remove(path)
		return nil, failed(nameGeneric, message)
	}
	return &videoEncoder{ctx: ctx, path: path, file: file, native: native}, nil
}

func (e *videoEncoder) append(shown *raster, milliseconds float64) error {
	if err := e.ctx.Err(); err != nil {
		return err
	}
	if message := C.swipt_encoder_append(e.native, (*C.uint8_t)(&shown.pixels[0]), C.size_t(shown.stride), C.int64_t(math.Round(milliseconds*float64(time.Millisecond)))); message != nil {
		return failed(nameGeneric, message)
	}
	return nil
}

func (e *videoEncoder) finish() error {
	message := C.swipt_encoder_finish(e.native)
	e.native = nil
	closed := e.file.Close()
	if message != nil {
		return failed(nameGeneric, message)
	}
	if closed != nil {
		return fileFailure(closed, "graphics: finish ", filepath.Base(e.path))
	}
	return nil
}

func (e *videoEncoder) cancel() {
	if e.native != nil {
		C.swipt_encoder_cancel(e.native)
		e.native = nil
	}
	e.file.Close()
	os.Remove(e.path)
}
