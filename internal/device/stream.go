package device

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	N "github.com/sagernet/sing/common/network"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/display"
	"github.com/swipt-automation/swipt/internal/coredevice/hid"
	"github.com/swipt-automation/swipt/internal/rfb/media"

	"github.com/google/uuid"
)

const (
	startInterval     = time.Second
	startBackoffLimit = 10 * time.Second
	// The device's streams sent RTCP sender reports about once a second (iPadOS 27.2 24B5089g).
	silenceLimit = 3 * time.Second
)

var errVideoSilent = E.New("the device stopped sending video")

type streamManager struct {
	logger      logger.ContextLogger
	onStreaming func(bool)

	slot chan struct{}

	access     sync.Mutex
	open       map[*deviceStream]struct{}
	remote     map[*deviceStream]struct{}
	lastStart  time.Time
	failures   int
	parked     *deviceStream
	streaming  bool
	generation uint64
	readyAt    time.Time
}

type deviceStream struct {
	ctx      context.Context
	device   *coredevice.Device
	stream   *display.Stream
	video    bool
	partner  *deviceStream
	closed   bool
	owner    *conn
	stopping time.Time
}

func newStreamManager(logger logger.ContextLogger, onStreaming func(bool)) *streamManager {
	return &streamManager{
		logger:      logger,
		onStreaming: onStreaming,
		slot:        make(chan struct{}, 1),
		open:        make(map[*deviceStream]struct{}),
		remote:      make(map[*deviceStream]struct{}),
	}
}

func (m *streamManager) acquire(ctx context.Context) error {
	select {
	case m.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *streamManager) release() {
	<-m.slot
}

func (m *streamManager) stop(stream *deviceStream) {
	_ = stream.stream.Close()
	err := stream.stream.WaitStopped()
	m.access.Lock()
	delete(m.remote, stream)
	m.streamingStoppedLocked()
	last := !slices.ContainsFunc(slices.Collect(maps.Keys(m.remote)), func(other *deviceStream) bool { return other.device == stream.device })
	m.access.Unlock()
	if last && stream.owner != nil {
		stream.owner.connection.viewing.ended(stream.owner, stream.stopping)
	}
	info := stream.stream.Info()
	switch {
	case err == nil:
		m.logger.Debug("stopped the device's ", string(info.Type), " stream ", info.SSRC)
	case stream.ctx.Err() != nil || stream.device.Err() != nil:
		m.logger.Debug(E.Cause(err, "stop the device's ", string(info.Type), " stream ", info.SSRC))
	default:
		m.logger.Error(E.Cause(err, "stop the device's ", string(info.Type), " stream ", info.SSRC))
	}
}

func (m *streamManager) discard(a *conn, stream *display.Stream) error {
	go m.stop(&deviceStream{ctx: a.owner, device: a.device, stream: stream, owner: a, stopping: time.Now()})
	return stream.Close()
}

func (m *streamManager) retire(stream *deviceStream) (retired bool) {
	m.access.Lock()
	retired = stream.closed
	m.retireLocked(stream)
	m.access.Unlock()
	return
}

func (m *streamManager) retireLocked(stream *deviceStream) {
	if stream.closed {
		return
	}
	stream.closed = true
	stream.stopping = time.Now()
	delete(m.open, stream)
	go m.stop(stream)
	switch {
	case m.parked == stream:
		m.parked = nil
	case m.parked != nil && m.parked.partner == stream:
		parked := m.parked
		m.parked = nil
		m.retireLocked(parked)
	}
	if stream.video {
		m.videoStoppedLocked()
	}
}

func (m *streamManager) stopDevice(device *coredevice.Device) error {
	m.retireDevice(device)
	_ = m.acquire(context.Background())
	defer m.release()
	m.retireDevice(device)
	m.access.Lock()
	var stopping []*deviceStream
	for stream := range m.remote {
		if stream.device == device {
			stopping = append(stopping, stream)
		}
	}
	m.access.Unlock()
	var err error
	for _, stream := range stopping {
		err = E.Errors(err, stream.stream.Close())
	}
	return err
}

func (m *streamManager) retireDevice(device *coredevice.Device) {
	m.access.Lock()
	defer m.access.Unlock()
	for stream := range m.open {
		if stream.device == device {
			m.retireLocked(stream)
		}
	}
}

func (m *streamManager) videoStoppedLocked() {
	for stream := range m.open {
		if stream.video {
			return
		}
	}
	m.generation++
	m.readyAt = time.Time{}
}

func (m *streamManager) streamingStoppedLocked() {
	if !m.streaming {
		return
	}
	for stream := range m.remote {
		if stream.video {
			return
		}
	}
	m.streaming = false
	m.onStreaming(false)
}

func (m *streamManager) videoStartedLocked(now time.Time) {
	m.generation++
	m.readyAt = now.Add(hid.StreamWarmup)
	if m.streaming {
		return
	}
	m.streaming = true
	m.onStreaming(true)
}

func (m *streamManager) inputState() (running bool, wait time.Duration, generation uint64) {
	m.access.Lock()
	defer m.access.Unlock()
	if m.readyAt.IsZero() {
		return false, 0, m.generation
	}
	return true, time.Until(m.readyAt), m.generation
}

func (m *streamManager) videoRunning() bool {
	m.access.Lock()
	defer m.access.Unlock()
	return !m.readyAt.IsZero()
}

func (m *streamManager) takeParked() *deviceStream {
	m.access.Lock()
	defer m.access.Unlock()
	parked := m.parked
	m.parked = nil
	if parked != nil {
		parked.partner = nil
	}
	return parked
}

func (m *streamManager) startDelay() time.Duration {
	m.access.Lock()
	defer m.access.Unlock()
	if m.lastStart.IsZero() {
		return 0
	}
	return time.Until(m.lastStart.Add(min(startInterval<<min(m.failures, 4), startBackoffLimit)))
}

func (m *streamManager) retireAll() {
	m.access.Lock()
	for stream := range m.open {
		m.retireLocked(stream)
	}
	stopping := slices.Collect(maps.Keys(m.remote))
	m.access.Unlock()
	for _, stream := range stopping {
		_ = stream.stream.Close()
	}
}

func (s *Connection) openVideo(ctx context.Context, request media.ServerVideoRequest) (media.ServerVideoSource, error) {
	a, release, err := s.acquire(ctx)
	if err != nil {
		return nil, err
	}
	release()
	m := s.streams
	if err = m.acquire(ctx); err != nil {
		return nil, err
	}
	type started struct {
		source *videoSource
		err    error
	}
	result := make(chan started, 1)
	go func() {
		defer m.release()
		source, err := s.startVideo(ctx, a, request)
		result <- started{source: source, err: err}
	}()
	select {
	case r := <-result:
		if r.err != nil {
			return nil, r.err
		}
		return r.source, nil
	case <-ctx.Done():
		go func() {
			if r := <-result; r.source != nil {
				_ = r.source.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

func (s *Connection) startVideo(ctx context.Context, a *conn, request media.ServerVideoRequest) (*videoSource, error) {
	m := s.streams
	m.retireAll()
	if err := sleep(ctx, m.startDelay()); err != nil {
		return nil, err
	}
	if a.ctx.Err() != nil {
		return nil, E.New("the device connection ended")
	}
	if err := a.darwin.waitReady(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		s.logger.Warn(E.Cause(err, "observe screen-viewing notifications"))
	}
	device := a.device
	sessionID := uuid.New()
	readOptions := N.ReadWaitOptions{RearHeadroom: media.ServerRearHeadroom}
	var audio *display.Stream
	var err error
	if request.Audio {
		// The device returned a nonzero video SyncStreamToken with audio, zero without it (iPadOS 27.2 24B5089g).
		audio, err = a.display.StartAudio(ctx, display.StreamOptions{SessionID: sessionID, ReadOptions: readOptions})
		if err != nil {
			s.logger.Warn(E.Cause(err, "start the device's sound"))
		}
	}
	m.access.Lock()
	m.lastStart = time.Now()
	m.access.Unlock()
	video, err := a.display.StartVideo(ctx, display.StreamOptions{
		SessionID:      sessionID,
		SSRC:           request.ClientSSRC,
		Mode:           deviceMode(request.Mode),
		TilesPerFrame:  request.TilesPerFrame,
		MaximumBitrate: uint64(max(request.MaximumBitrate, 0)),
		PixelFormats:   uint32(request.PixelFormats),
		ReadOptions:    readOptions,
	})
	if err != nil {
		if audio != nil {
			err = E.Errors(err, m.discard(a, audio))
		}
		if ctx.Err() == nil {
			m.access.Lock()
			m.failures++
			m.access.Unlock()
			s.setVideoProblem(err)
		}
		return nil, E.Cause(err, "start the device's video")
	}
	if ctx.Err() != nil {
		err = E.Errors(ctx.Err(), m.discard(a, video))
		if audio != nil {
			err = E.Errors(err, m.discard(a, audio))
		}
		return nil, err
	}
	info := video.Info()
	now := time.Now()
	source := &videoSource{connection: s, stream: &deviceStream{ctx: a.owner, device: device, stream: video, video: true, owner: a}}
	source.lastHeard.Store(now.UnixNano())
	source.info = media.ServerVideoInfo{
		SSRC:          info.DeviceSSRC,
		ReceiverSSRC:  info.SSRC,
		Width:         info.Width,
		Height:        info.Height,
		LTRP:          info.LTRP,
		FrameRate:     info.FrameRate,
		PixelFormat:   pixelFormat(info.PixelFormat),
		TilesPerFrame: info.TilesPerFrame,
	}
	if source.info.PixelFormat == 0 {
		s.logger.Warn("the device's video pixel format ", fmt.Sprintf("%#x", info.PixelFormat), " is unknown; the viewer is told 4:2:0")
	}
	m.access.Lock()
	m.failures = 0
	m.open[source.stream] = struct{}{}
	m.remote[source.stream] = struct{}{}
	if audio != nil {
		parked := &deviceStream{ctx: a.owner, device: device, stream: audio, partner: source.stream, owner: a}
		m.open[parked] = struct{}{}
		m.remote[parked] = struct{}{}
		m.parked = parked
	}
	m.videoStartedLocked(now)
	m.access.Unlock()
	s.logger.Info("device video started: SSRC ", info.SSRC, ", device SSRC ", info.DeviceSSRC,
		", ", info.Width, "x", info.Height, " at ", info.FrameRate, " fps, LTRP ", info.LTRP,
		", mode ", request.Mode, ", ", info.TilesPerFrame, " tiles, pixel format ", uint32(source.info.PixelFormat), ", maximum bitrate ", request.MaximumBitrate,
		", sound ", audio != nil, ", sync token ", info.SyncToken)
	s.setVideoProblem(nil)
	s.videoSize(info.Width, info.Height)
	s.checkDisplay()
	s.input.videoReady()
	s.screens.viewerStarted(source)
	return source, nil
}

func (s *Connection) openAudio() (media.ServerAudioSource, error) {
	parked := s.streams.takeParked()
	if parked == nil {
		return nil, media.ErrAudioUnsupported
	}
	info := parked.stream.Info()
	return &audioSource{
		manager: s.streams,
		stream:  parked,
		info:    media.ServerAudioInfo{SSRC: info.DeviceSSRC, ReceiverSSRC: info.SSRC},
	}, nil
}

func deviceMode(mode media.VideoMode) display.Mode {
	switch mode {
	case media.VideoModeNearby:
		return display.ModeNearby
	case media.VideoModeCoreDevice:
		return display.ModeCoreDevice
	}
	return display.ModeRemoteDesktop
}

func pixelFormat(format uint32) media.PixelFormats {
	switch format {
	case 0x34323076:
		return media.PixelFormat420VideoRange
	case 0x34323066:
		return media.PixelFormat420FullRange
	case 0x34343476:
		return media.PixelFormat444VideoRange
	case 0x34343466:
		return media.PixelFormat444FullRange
	}
	return 0
}

type videoSource struct {
	connection *Connection
	stream     *deviceStream
	info       media.ServerVideoInfo
	lastHeard  atomic.Int64

	readContext                 context.Context
	packets                     []display.Packet
	pictureWidth, pictureHeight int
}

func (v *videoSource) Info() media.ServerVideoInfo {
	return v.info
}

func (v *videoSource) ReadPackets(ctx context.Context, packets []media.ServerPacket) (int, error) {
	if v.readContext == nil {
		readContext, cancel := context.WithCancelCause(ctx)
		v.readContext = readContext
		v.watchSilence(readContext, cancel)
	}
	if len(v.packets) < len(packets) {
		v.packets = make([]display.Packet, len(packets))
	}
	n, err := v.stream.stream.ReadPackets(v.readContext, v.packets[:len(packets)])
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		if errors.Is(context.Cause(v.readContext), errVideoSilent) {
			v.connection.logger.Warn("the device's video went silent; another client such as Xcode's Device Hub may have taken the screen")
			v.connection.setVideoProblem(&Error{Problem: ProblemScreenInUse, Err: errVideoSilent})
			return 0, errVideoSilent
		}
		return 0, err
	}
	v.lastHeard.Store(time.Now().UnixNano())
	screens := v.connection.screens
	sharing := screens.sharesViewer(v)
	var batch []byte
	for i, packet := range v.packets[:n] {
		if !packet.RTCP {
			screens.observe(packet.Buffer.Bytes(), packet.Arrival)
			if sps := hevcSPS(rtpPayload(packet.Buffer.Bytes())); sps != nil {
				v.checkPictureSize(sps)
			}
		}
		if sharing {
			batch = appendPacketRecord(batch, packet.Buffer.Bytes(), packet.RTCP, packet.Arrival)
		}
		packets[i] = media.ServerPacket{Buffer: packet.Buffer, RTCP: packet.RTCP, Arrival: packet.Arrival}
	}
	if sharing {
		screens.deliver(batch, func() bool { return screens.viewer == v })
	}
	return n, nil
}

func (v *videoSource) checkPictureSize(sps []byte) {
	width, height, ok := media.HEVCPictureSize(sps)
	if !ok || width == 0 || height == 0 || width > largestPicture || height > largestPicture {
		return
	}
	if v.pictureWidth == 0 {
		v.pictureWidth, v.pictureHeight = width, height
		v.connection.logger.Debug("video size in its parameter sets: ", width, "x", height, ", started at ", v.info.Width, "x", v.info.Height)
		return
	}
	if width == v.pictureWidth && height == v.pictureHeight {
		return
	}
	v.pictureWidth, v.pictureHeight = width, height
	v.connection.logger.Info("the device's video changed size: ", width, "x", height)
	v.connection.checkDisplay()
	v.connection.videoSize(width, height)
}

func (v *videoSource) watchSilence(readContext context.Context, cancel context.CancelCauseFunc) {
	if readContext.Err() != nil {
		return
	}
	wait := time.Until(time.Unix(0, v.lastHeard.Load()).Add(silenceLimit))
	if wait <= 0 {
		cancel(errVideoSilent)
		return
	}
	time.AfterFunc(wait, func() { v.watchSilence(readContext, cancel) })
}

func (v *videoSource) WriteRTCP(packet []byte) error {
	return v.stream.stream.WriteRTCP(packet)
}

func (v *videoSource) RequestKeyFrame() error {
	return v.stream.stream.RequestKeyFrame()
}

func (v *videoSource) Close() error {
	v.connection.streams.retire(v.stream)
	v.connection.screens.viewerStopped(v)
	return nil
}

type audioSource struct {
	manager *streamManager
	stream  *deviceStream
	info    media.ServerAudioInfo
	packets []display.Packet
}

func (a *audioSource) Info() media.ServerAudioInfo {
	return a.info
}

func (a *audioSource) ReadPackets(ctx context.Context, packets []media.ServerPacket) (int, error) {
	if len(a.packets) < len(packets) {
		a.packets = make([]display.Packet, len(packets))
	}
	n, err := a.stream.stream.ReadPackets(ctx, a.packets[:len(packets)])
	for i, packet := range a.packets[:n] {
		packets[i] = media.ServerPacket{Buffer: packet.Buffer, RTCP: packet.RTCP, Arrival: packet.Arrival}
	}
	return n, err
}

func (a *audioSource) WriteRTCP(packet []byte) error {
	return a.stream.stream.WriteRTCP(packet)
}

func (a *audioSource) Close() error {
	a.manager.retire(a.stream)
	return nil
}
