//go:build android

package android

import (
	"bytes"
	"context"
	"maps"
	"math"
	"slices"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/rfb/media"
	"github.com/swipt-automation/swipt/internal/screen"
)

const (
	defaultScreenBitrate = 10_000_000
	keyFrameInterval     = 500 * time.Millisecond
)

type ScreenConsumer interface {
	Format(width int32, height int32, scale float64, encodedWidth int32, encodedHeight int32, encodedScale float64)
	Unit(data []byte, timeUs int64, turns int32)
	Drawn(nanoseconds int64, turns int32)
	WindowFailed(window int64, message string)
	Ended(message string)
}

type ScreenCapture interface {
	RequestKeyFrame()
	SetBitrate(bitrate int32)
	AddWindow(window int64) error
	RemoveWindow(window int64)
	SetEncoding(encoding bool)
	Close()
}

type screens struct {
	device  *Device
	display int32
	access  sync.Mutex
	open    map[*ScreenStream]struct{}
	capture *screenCapture
	windows map[uintptr]screenWindow
}

type screenWindow struct {
	stream *ScreenStream
	drawn  func(nanoseconds int64, turns int)
	failed func(error)
}

type screenCapture struct {
	screens  *screens
	capture  ScreenCapture
	ready    chan struct{}
	failure  error
	info     screen.Info
	encoded  screen.Info
	bitrate  uint64
	keyFrame time.Time
}

type ScreenStream struct {
	screens  *screens
	info     screen.Info
	encoded  screen.Info
	bitrate  uint64
	consumer screen.Consumer
	windows  map[uintptr]struct{}
	encoding bool
}

func (d *Device) OpenScreenStream(ctx context.Context, options screen.Options, consumer screen.Consumer) (screen.Stream, error) {
	display := int32(options.Display)
	d.screensAccess.Lock()
	s := d.screens[display]
	if s == nil {
		s = &screens{device: d, display: display, open: make(map[*ScreenStream]struct{}), windows: make(map[uintptr]screenWindow)}
		d.screens[display] = s
	}
	stream := &ScreenStream{screens: s, bitrate: options.MaximumBitrate, consumer: consumer, windows: make(map[uintptr]struct{})}
	s.access.Lock()
	d.screensAccess.Unlock()
	s.open[stream] = struct{}{}
	var starting *screenCapture
	if s.capture == nil {
		starting = &screenCapture{screens: s, ready: make(chan struct{}), bitrate: s.bitrateLocked()}
		s.capture = starting
	} else {
		s.adjustBitrateLocked()
	}
	capture := s.capture
	s.access.Unlock()
	if starting != nil {
		go starting.start()
	}
	select {
	case <-capture.ready:
	case <-ctx.Done():
		stream.Close()
		return nil, ctx.Err()
	}
	s.access.Lock()
	err := capture.failure
	stream.info, stream.encoded = capture.info, capture.encoded
	s.access.Unlock()
	if err != nil {
		stream.Close()
		return nil, err
	}
	return stream, nil
}

func (c *screenCapture) start() {
	capture, err := c.screens.device.Layer.OpenScreen(c.screens.display, int32(min(c.bitrate, math.MaxInt32)), c)
	s := c.screens
	s.access.Lock()
	if err != nil || s.capture != c {
		s.access.Unlock()
		if capture != nil {
			capture.Close()
		}
		if err != nil {
			c.Ended(Failure(err).Error())
		}
		return
	}
	c.capture = capture
	windows := slices.Collect(maps.Keys(s.windows))
	encoding := s.encodingLocked()
	s.access.Unlock()
	for _, window := range windows {
		if err := capture.AddWindow(int64(window)); err != nil {
			c.windowFailed(window, err)
		}
	}
	capture.SetEncoding(encoding)
}

func (c *screenCapture) windowFailed(window uintptr, err error) {
	s := c.screens
	s.access.Lock()
	drawing, found := s.windows[window]
	if !found || s.capture != c {
		s.access.Unlock()
		return
	}
	delete(s.windows, window)
	delete(drawing.stream.windows, window)
	s.access.Unlock()
	go drawing.failed(err)
}

func (c *screenCapture) Format(width int32, height int32, scale float64, encodedWidth int32, encodedHeight int32, encodedScale float64) {
	info := screen.Info{Width: int(width), Height: int(height), Scale: scale}
	encoded := screen.Info{Width: int(encodedWidth), Height: int(encodedHeight), Scale: encodedScale}
	s := c.screens
	s.access.Lock()
	moved := c.info != (screen.Info{}) && (c.info != info || c.encoded != encoded)
	c.info, c.encoded = info, encoded
	select {
	case <-c.ready:
	default:
		close(c.ready)
	}
	var streams []*ScreenStream
	if s.capture == c {
		streams = slices.Collect(maps.Keys(s.open))
		for _, stream := range streams {
			stream.info, stream.encoded = info, encoded
		}
	}
	s.access.Unlock()
	if moved {
		for _, stream := range streams {
			stream.consumer.ScreenMoved(info)
		}
	}
}

func (c *screenCapture) Unit(data []byte, timeUs int64, turns int32) {
	s := c.screens
	s.access.Lock()
	if s.capture != c {
		s.access.Unlock()
		return
	}
	streams := slices.Collect(maps.Keys(s.open))
	s.access.Unlock()
	unit := &media.AccessUnit{
		NALs:    annexB(bytes.Clone(data)),
		Time:    timeUs * media.VideoClockRate / int64(time.Second/time.Microsecond),
		Arrival: time.Now(),
		Turns:   int(turns) & 3,
	}
	for _, stream := range streams {
		stream.consumer.ScreenUnit(unit)
	}
}

func (c *screenCapture) Drawn(nanoseconds int64, turns int32) {
	s := c.screens
	s.access.Lock()
	var drawn []func(int64, int)
	if s.capture == c {
		for _, window := range s.windows {
			drawn = append(drawn, window.drawn)
		}
	}
	s.access.Unlock()
	for _, notify := range drawn {
		notify(nanoseconds, int(turns)&3)
	}
}

func (c *screenCapture) WindowFailed(window int64, message string) {
	c.windowFailed(uintptr(window), E.New(message))
}

func (c *screenCapture) Ended(message string) {
	err := E.New("the screen capture ended: ", message)
	s := c.screens
	s.access.Lock()
	c.failure = err
	select {
	case <-c.ready:
	default:
		close(c.ready)
	}
	if s.capture != c {
		s.access.Unlock()
		return
	}
	streams := s.open
	s.open = make(map[*ScreenStream]struct{})
	clear(s.windows)
	s.capture = nil
	capture := c.capture
	s.access.Unlock()
	s.release()
	if capture != nil {
		capture.Close()
	}
	for stream := range streams {
		stream.consumer.ScreenEnded(err)
	}
}

func (s *ScreenStream) Info() screen.Info {
	s.screens.access.Lock()
	defer s.screens.access.Unlock()
	return s.info
}

func (s *ScreenStream) RequestKeyFrame() {
	s.screens.access.Lock()
	capture := s.screens.capture
	var request ScreenCapture
	if capture != nil && capture.capture != nil && time.Since(capture.keyFrame) >= keyFrameInterval {
		capture.keyFrame = time.Now()
		request = capture.capture
	}
	s.screens.access.Unlock()
	if request != nil {
		request.RequestKeyFrame()
	}
}

func (s *ScreenStream) EncodedInfo() screen.Info {
	s.screens.access.Lock()
	defer s.screens.access.Unlock()
	return s.encoded
}

func (s *ScreenStream) Draw(window uintptr, drawn func(nanoseconds int64, turns int), failed func(error)) {
	screens := s.screens
	screens.access.Lock()
	if _, open := screens.open[s]; !open {
		screens.access.Unlock()
		return
	}
	s.windows[window] = struct{}{}
	screens.windows[window] = screenWindow{stream: s, drawn: drawn, failed: failed}
	current := screens.capture
	capture := screens.liveCaptureLocked()
	screens.access.Unlock()
	if capture != nil {
		if err := capture.AddWindow(int64(window)); err != nil {
			current.windowFailed(window, err)
		}
	}
}

func (s *ScreenStream) StopDrawing(window uintptr) {
	screens := s.screens
	screens.access.Lock()
	if _, drawing := s.windows[window]; !drawing {
		screens.access.Unlock()
		return
	}
	delete(s.windows, window)
	delete(screens.windows, window)
	capture := screens.liveCaptureLocked()
	screens.access.Unlock()
	if capture != nil {
		capture.RemoveWindow(int64(window))
	}
}

func (s *ScreenStream) Encode(enabled bool) {
	screens := s.screens
	screens.access.Lock()
	if _, open := screens.open[s]; !open || s.encoding == enabled {
		screens.access.Unlock()
		return
	}
	s.encoding = enabled
	encoding := screens.encodingLocked()
	capture := screens.liveCaptureLocked()
	screens.access.Unlock()
	if capture != nil {
		capture.SetEncoding(encoding)
	}
}

func (s *ScreenStream) Close() {
	for window := range s.drawingWindows() {
		s.StopDrawing(window)
	}
	s.Encode(false)
	screens := s.screens
	screens.access.Lock()
	if _, open := screens.open[s]; !open {
		screens.access.Unlock()
		return
	}
	delete(screens.open, s)
	var closing ScreenCapture
	if len(screens.open) == 0 && screens.capture != nil {
		closing = screens.capture.capture
		screens.capture = nil
	} else {
		screens.adjustBitrateLocked()
	}
	screens.access.Unlock()
	screens.release()
	if closing != nil {
		closing.Close()
	}
}

func (s *screens) release() {
	s.device.screensAccess.Lock()
	defer s.device.screensAccess.Unlock()
	s.access.Lock()
	defer s.access.Unlock()
	if len(s.open) == 0 && s.capture == nil && s.device.screens[s.display] == s {
		delete(s.device.screens, s.display)
	}
}

func (s *ScreenStream) drawingWindows() map[uintptr]struct{} {
	s.screens.access.Lock()
	defer s.screens.access.Unlock()
	return maps.Clone(s.windows)
}

func (s *screens) liveCaptureLocked() ScreenCapture {
	if s.capture == nil {
		return nil
	}
	return s.capture.capture
}

func (s *screens) encodingLocked() bool {
	for stream := range s.open {
		if stream.encoding {
			return true
		}
	}
	return false
}

func (s *screens) bitrateLocked() uint64 {
	var bitrate uint64
	for stream := range s.open {
		bitrate = max(bitrate, stream.bitrate)
	}
	if bitrate == 0 {
		return defaultScreenBitrate
	}
	return bitrate
}

func (s *screens) adjustBitrateLocked() {
	capture := s.capture
	bitrate := s.bitrateLocked()
	if capture == nil || capture.bitrate == bitrate {
		return
	}
	capture.bitrate = bitrate
	if capture.capture != nil {
		go capture.capture.SetBitrate(int32(min(bitrate, math.MaxInt32)))
	}
}

func annexB(payload []byte) [][]byte {
	var nals [][]byte
	start := -1
	add := func(end int) {
		for end > start && payload[end-1] == 0 {
			end--
		}
		if end-start >= 2 {
			nals = append(nals, payload[start:end])
		}
	}
	for at := 0; at+2 < len(payload); {
		if payload[at] != 0 || payload[at+1] != 0 || payload[at+2] != 1 {
			at++
			continue
		}
		if start >= 0 {
			add(at)
		}
		at += 3
		start = at
	}
	if start >= 0 {
		add(len(payload))
	}
	return nals
}
