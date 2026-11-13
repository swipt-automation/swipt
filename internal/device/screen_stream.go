package device

import (
	"context"
	"encoding/binary"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/display"
	"github.com/swipt-automation/swipt/internal/rfb/media"

	"github.com/google/uuid"
)

const (
	captureStartLimit    = 20 * time.Second
	captureStartAttempts = 3
	captureStableRun     = 10 * time.Second
	captureRetryDelay    = time.Second
	captureBatch         = 64

	defaultScreenBitrate = 10_000_000
)

type ScreenMode string

const (
	ScreenModeRemoteDesktop ScreenMode = "remoteDesktop"
	ScreenModeCoreDevice    ScreenMode = "coreDevice"
)

type ScreenStreamOptions struct {
	Mode           ScreenMode
	MaximumBitrate uint64
}

type ScreenInfo struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
}

type ScreenStream struct {
	screens *screenStreams
	info    ScreenInfo
	options ScreenStreamOptions
	deliver func(batch []byte)
	end     func(err error)
	release func()
}

type screenCapture struct {
	cancel  context.CancelFunc
	stream  *deviceStream
	options ScreenStreamOptions
	restart bool
}

type screenStreams struct {
	connection *Connection
	access     sync.Mutex
	open       map[*ScreenStream]struct{}
	viewer     *videoSource
	capture    *screenCapture

	orientation atomic.Int32
	oriented    atomic.Int64
	seen        atomic.Int64
}

func newScreenStreams(connection *Connection) *screenStreams {
	return &screenStreams{connection: connection, open: make(map[*ScreenStream]struct{})}
}

func (s *Connection) OpenScreenStream(ctx context.Context, options ScreenStreamOptions, deliver func(batch []byte), end func(err error)) (*ScreenStream, error) {
	c, release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	displays, err := c.display.Displays(ctx)
	release()
	if err != nil {
		return nil, E.Cause(err, "read the device's displays")
	}
	primary, found := primaryDisplay(displays)
	if !found {
		return nil, E.New("the device lists no usable screen")
	}
	stream := &ScreenStream{
		screens: s.screens,
		info:    ScreenInfo{Width: primary.Width, Height: primary.Height, Scale: float64(max(primary.PointScale, 1))},
		options: options,
		deliver: deliver,
		end:     end,
		release: s.hold(),
	}
	s.screens.add(stream)
	return stream, nil
}

func (s *ScreenStream) Info() ScreenInfo {
	return s.info
}

func (s *ScreenStream) RequestKeyFrame() {
	s.screens.requestKeyFrame()
}

func (s *ScreenStream) Configure(options ScreenStreamOptions) {
	s.screens.configure(s, options)
}

func (s *ScreenStream) Close() {
	s.screens.remove(s)
	s.release()
}

func (s *screenStreams) add(stream *ScreenStream) {
	s.access.Lock()
	defer s.access.Unlock()
	s.open[stream] = struct{}{}
	switch {
	case s.viewer != nil:
		_ = s.viewer.RequestKeyFrame()
	case s.capture == nil:
		s.startCaptureLocked()
	case s.capture.stream != nil:
		_ = s.capture.stream.stream.RequestKeyFrame()
	}
	s.restartLocked()
}

func (s *screenStreams) remove(stream *ScreenStream) {
	s.access.Lock()
	defer s.access.Unlock()
	delete(s.open, stream)
	if len(s.open) == 0 && s.capture != nil {
		s.capture.cancel()
		s.capture = nil
		return
	}
	s.restartLocked()
}

func (s *screenStreams) configure(stream *ScreenStream, options ScreenStreamOptions) {
	s.access.Lock()
	defer s.access.Unlock()
	if _, open := s.open[stream]; !open {
		return
	}
	stream.options = options
	s.restartLocked()
}

func (s *screenStreams) mergedLocked() ScreenStreamOptions {
	options := make([]ScreenStreamOptions, 0, len(s.open))
	for stream := range s.open {
		options = append(options, stream.options)
	}
	return MergeScreenStreamOptions(options)
}

func MergeScreenStreamOptions(options []ScreenStreamOptions) ScreenStreamOptions {
	merged := ScreenStreamOptions{Mode: ScreenModeCoreDevice}
	for _, option := range options {
		if option.Mode != ScreenModeCoreDevice {
			merged.Mode = ScreenModeRemoteDesktop
			merged.MaximumBitrate = max(merged.MaximumBitrate, option.MaximumBitrate)
		}
	}
	if merged.Mode == ScreenModeRemoteDesktop && merged.MaximumBitrate == 0 {
		merged.MaximumBitrate = defaultScreenBitrate
	}
	return merged
}

func (s *screenStreams) restartLocked() {
	capture := s.capture
	if capture == nil || capture.stream == nil || capture.restart || capture.options == s.mergedLocked() {
		return
	}
	capture.restart = true
	go s.connection.streams.retire(capture.stream)
}

func (s *screenStreams) startCaptureLocked() {
	ctx, cancel := context.WithCancel(s.connection.ctx)
	capture := &screenCapture{cancel: cancel}
	s.capture = capture
	go s.runCapture(ctx, capture)
}

func (s *screenStreams) requestKeyFrame() {
	s.access.Lock()
	defer s.access.Unlock()
	switch {
	case s.viewer != nil:
		_ = s.viewer.RequestKeyFrame()
	case s.capture != nil && s.capture.stream != nil:
		_ = s.capture.stream.stream.RequestKeyFrame()
	}
}

func (s *screenStreams) viewerStarted(v *videoSource) {
	s.access.Lock()
	defer s.access.Unlock()
	s.viewer = v
}

func (s *screenStreams) viewerStopped(v *videoSource) {
	s.access.Lock()
	defer s.access.Unlock()
	if s.viewer != v {
		return
	}
	s.viewer = nil
	if len(s.open) > 0 && s.capture == nil {
		s.startCaptureLocked()
	}
}

func (s *screenStreams) viewerTaken() bool {
	s.access.Lock()
	defer s.access.Unlock()
	return s.viewer != nil
}

func (s *screenStreams) sharesViewer(v *videoSource) bool {
	s.access.Lock()
	defer s.access.Unlock()
	return s.viewer == v && len(s.open) > 0
}

func (s *screenStreams) runCapture(ctx context.Context, capture *screenCapture) {
	defer func() {
		s.access.Lock()
		if s.capture == capture {
			s.capture = nil
			if ctx.Err() == nil && s.viewer == nil && len(s.open) > 0 {
				s.startCaptureLocked()
			}
		}
		s.access.Unlock()
		capture.cancel()
	}()
	failures := 0
	for ctx.Err() == nil {
		started := time.Now()
		s.access.Lock()
		options := s.mergedLocked()
		s.access.Unlock()
		stream, err := s.connection.startCapture(ctx, options)
		if stream == nil && err == nil {
			return
		}
		restart := false
		if err == nil {
			s.access.Lock()
			capture.stream = stream
			capture.options = options
			s.restartLocked()
			s.access.Unlock()
			err = s.read(ctx, capture, stream)
			s.access.Lock()
			capture.stream = nil
			restart = capture.restart
			capture.restart = false
			s.access.Unlock()
			if s.connection.streams.retire(stream) {
				restart = true
			}
		}
		if ctx.Err() != nil || s.viewerTaken() {
			return
		}
		if restart {
			failures = 0
			continue
		}
		if time.Since(started) >= captureStableRun {
			failures = 0
		}
		failures++
		if failures >= captureStartAttempts {
			s.fail(capture, err)
			return
		}
		s.connection.logger.Debug(E.Cause(err, "the screen capture stopped; restarting it"))
		if sleep(ctx, captureRetryDelay) != nil {
			return
		}
	}
}

func (s *screenStreams) fail(capture *screenCapture, err error) {
	s.access.Lock()
	if s.capture != capture {
		s.access.Unlock()
		return
	}
	streams := s.open
	s.open = make(map[*ScreenStream]struct{})
	s.capture = nil
	s.access.Unlock()
	for stream := range streams {
		stream.end(err)
		stream.release()
	}
}

func (s *screenStreams) read(ctx context.Context, capture *screenCapture, stream *deviceStream) error {
	packets := make([]display.Packet, captureBatch)
	for {
		readContext, cancel := context.WithTimeoutCause(ctx, silenceLimit, errVideoSilent)
		n, err := stream.stream.ReadPackets(readContext, packets)
		cancel()
		if err != nil {
			return err
		}
		var batch []byte
		for _, packet := range packets[:n] {
			if !packet.RTCP {
				s.observe(packet.Buffer.Bytes(), packet.Arrival)
			}
			batch = appendPacketRecord(batch, packet.Buffer.Bytes(), packet.RTCP, packet.Arrival)
			packet.Buffer.Release()
		}
		s.deliver(batch, func() bool { return s.capture == capture })
	}
}

func (s *screenStreams) deliver(batch []byte, current func() bool) {
	s.access.Lock()
	if !current() {
		s.access.Unlock()
		return
	}
	streams := slices.Collect(maps.Keys(s.open))
	s.access.Unlock()
	for _, stream := range streams {
		stream.deliver(batch)
	}
}

func (s *screenStreams) observe(packet []byte, arrival time.Time) {
	orientation, found := media.PacketOrientation(packet)
	if !found {
		return
	}
	at := arrival.UnixNano()
	if s.orientation.Swap(int32(orientation)+1) != int32(orientation)+1 {
		s.oriented.Store(at)
	}
	s.seen.Store(at)
}

func (s *screenStreams) steadySince(at time.Time) bool {
	return time.Since(time.Unix(0, s.seen.Load())) < phoneGeometryLife && s.oriented.Load() < at.UnixNano()
}

func appendPacketRecord(batch []byte, data []byte, rtcp bool, arrival time.Time) []byte {
	batch = binary.BigEndian.AppendUint64(batch, uint64(arrival.UnixNano()))
	flag := byte(0)
	if rtcp {
		flag = 1
	}
	batch = append(batch, flag)
	batch = binary.BigEndian.AppendUint32(batch, uint32(len(data)))
	return append(batch, data...)
}

func (s *Connection) startCapture(ctx context.Context, options ScreenStreamOptions) (*deviceStream, error) {
	ctx, cancel := context.WithTimeout(ctx, captureStartLimit)
	defer cancel()
	a, release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	release()
	m := s.streams
	if err = m.acquire(ctx); err != nil {
		return nil, err
	}
	defer m.release()
	if s.screens.viewerTaken() {
		return nil, nil
	}
	m.retireAll()
	if err = sleep(ctx, m.startDelay()); err != nil {
		return nil, err
	}
	m.access.Lock()
	m.lastStart = time.Now()
	m.access.Unlock()
	if err = a.darwin.waitReady(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s.logger.Warn(E.Cause(err, "observe screen-viewing notifications"))
	}
	mode := display.ModeRemoteDesktop
	if options.Mode == ScreenModeCoreDevice {
		mode = display.ModeCoreDevice
	}
	video, err := a.display.StartVideo(ctx, display.StreamOptions{
		SessionID:      uuid.New(),
		Mode:           mode,
		MaximumBitrate: options.MaximumBitrate,
	})
	if err != nil {
		if ctx.Err() == nil {
			m.access.Lock()
			m.failures++
			m.access.Unlock()
		}
		return nil, E.Cause(err, "start the device's video")
	}
	stream := &deviceStream{ctx: a.owner, device: a.device, stream: video, owner: a}
	m.access.Lock()
	m.failures = 0
	m.open[stream] = struct{}{}
	m.remote[stream] = struct{}{}
	m.access.Unlock()
	_ = video.RequestKeyFrame()
	return stream, nil
}
