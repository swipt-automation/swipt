//go:build cgo

package graphics

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/rfb/media"
	"github.com/swipt-automation/swipt/internal/screen"
)

const (
	screenOpenLimit       = 20 * time.Second
	screenFirstFrameLimit = 5 * time.Second
	screenBacklog         = 256
)

var (
	errScreenSilent = failure(nameUnavailable, "graphics: the screen sent no frame within ", int(screenFirstFrameLimit/time.Second), " s; it may be off")
	errScreenClosed = failure(nameState, "graphics: the screen stream is closed")
	errScreenMoved  = failure(nameState, "graphics: the screen changed its size or moved to another display, which ended the recording")
)

type liveFrames interface {
	unit(unit *media.AccessUnit, format *hevcFormat, formatChanged bool, damaged bool) (needsKeyFrame bool)
	close()
}

type screenEvent struct {
	unit  *media.AccessUnit
	moved *screen.Info
}

type screenStream struct {
	engine   *engine
	device   screen.Stream
	info     screen.Info
	events   chan screenEvent
	dropped  atomic.Bool
	done     chan struct{}
	finished chan struct{}
	format   *hevcFormat

	decoding sync.Mutex
	frames   liveFrames

	access      sync.Mutex
	recordings  map[*recordingSession]struct{}
	latest      *imageNode
	sequence    int64
	changed     chan struct{}
	failure     error
	undecodable error
	released    bool
	closed      bool
}

func (e *engine) openScreen(ctx context.Context, options screen.Options) (*screenStream, error) {
	stream := &screenStream{
		engine:     e,
		events:     make(chan screenEvent, screenBacklog),
		done:       make(chan struct{}),
		finished:   make(chan struct{}),
		recordings: make(map[*recordingSession]struct{}),
		changed:    make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(ctx, screenOpenLimit)
	defer cancel()
	opened, err := e.graphics.screen.OpenScreenStream(ctx, options, stream)
	if err != nil {
		return nil, err
	}
	stream.device = opened
	stream.info = opened.Info()
	stream.decoding.Lock()
	stream.openFramesLocked()
	stream.decoding.Unlock()
	go stream.run()
	if !e.addStream(stream) {
		stream.close()
		return nil, automation.ErrSessionEnded
	}
	return stream, nil
}

func (s *screenStream) ScreenUnit(unit *media.AccessUnit) {
	select {
	case s.events <- screenEvent{unit: unit}:
	case <-s.done:
	default:
		s.dropped.Store(true)
	}
}

func (s *screenStream) ScreenMoved(info screen.Info) {
	select {
	case s.events <- screenEvent{moved: &info}:
	case <-s.done:
	}
}

func (s *screenStream) ScreenEnded(err error) {
	s.access.Lock()
	if s.closed || s.failure != nil {
		s.access.Unlock()
		return
	}
	s.failure = err
	recordings := slices.Collect(maps.Keys(s.recordings))
	latest := s.takeLatestLocked()
	s.access.Unlock()
	if latest != nil {
		latest.release()
	}
	for _, recording := range recordings {
		recording.fail(err)
	}
}

func (s *screenStream) run() {
	defer close(s.finished)
	for {
		select {
		case <-s.done:
			return
		case event := <-s.events:
			if event.moved != nil {
				s.move(*event.moved)
				continue
			}
			unit := event.unit
			damaged := s.dropped.Swap(false) || unit.Damaged
			formatChanged := s.adoptFormat(unit)
			needsKeyFrame := false
			for _, recording := range s.activeRecordings() {
				needsKeyFrame = recording.append(unit, s.format, damaged) || needsKeyFrame
			}
			s.decoding.Lock()
			if s.frames != nil {
				needsKeyFrame = s.frames.unit(unit, s.format, formatChanged, damaged) || needsKeyFrame
			}
			s.decoding.Unlock()
			if needsKeyFrame {
				s.device.RequestKeyFrame()
			}
		}
	}
}

func (s *screenStream) move(info screen.Info) {
	s.access.Lock()
	s.info = info
	recordings := slices.Collect(maps.Keys(s.recordings))
	s.access.Unlock()
	for _, recording := range recordings {
		recording.fail(errScreenMoved)
	}
	s.format = nil
	s.decoding.Lock()
	if s.frames != nil {
		s.frames.close()
		s.openFramesLocked()
	}
	s.decoding.Unlock()
}

func (s *screenStream) openFramesLocked() {
	frames, err := newLiveFrames(s)
	s.frames = frames
	s.access.Lock()
	s.undecodable = err
	s.access.Unlock()
}

func (s *screenStream) adoptFormat(unit *media.AccessUnit) bool {
	vps, sps, pps := unit.ParameterSets()
	if vps == nil || sps == nil || pps == nil {
		return false
	}
	if s.format != nil && bytes.Equal(vps, s.format.vps) && bytes.Equal(sps, s.format.source) && bytes.Equal(pps, s.format.pps) {
		return false
	}
	format, err := newHEVCFormat(vps, sps, pps, s.encodedInfo())
	if err != nil {
		s.engine.graphics.logger.Error("read the screen's parameter sets: ", err)
		return false
	}
	s.format = format
	return true
}

func (s *screenStream) activeRecordings() []*recordingSession {
	s.access.Lock()
	defer s.access.Unlock()
	return slices.Collect(maps.Keys(s.recordings))
}

func (s *screenStream) takeLatestLocked() *imageNode {
	latest := s.latest
	s.latest = nil
	close(s.changed)
	s.changed = make(chan struct{})
	return latest
}

func (s *screenStream) frameAfter(ctx context.Context, previous int64, limited bool) (*imageNode, int64, error) {
	var deadline <-chan time.Time
	if limited {
		timer := time.NewTimer(screenFirstFrameLimit)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		s.access.Lock()
		switch {
		case s.closed:
			s.access.Unlock()
			return nil, 0, errScreenClosed
		case s.failure != nil:
			err := s.failure
			s.access.Unlock()
			return nil, 0, err
		case s.undecodable != nil:
			err := s.undecodable
			s.access.Unlock()
			return nil, 0, err
		case s.latest != nil && s.sequence > previous:
			latest, sequence := s.latest.retain(), s.sequence
			s.access.Unlock()
			return latest, sequence, nil
		}
		changed := s.changed
		s.access.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		case <-deadline:
			return nil, 0, errScreenSilent
		}
	}
}

func (s *screenStream) addRecording(recording *recordingSession) error {
	s.access.Lock()
	switch {
	case s.closed:
		s.access.Unlock()
		return errScreenClosed
	case s.failure != nil:
		s.access.Unlock()
		return s.failure
	}
	s.recordings[recording] = struct{}{}
	recording.screen = s.encodedInfo()
	s.encodeLocked()
	s.access.Unlock()
	return nil
}

func (s *screenStream) removeRecording(recording *recordingSession) {
	s.access.Lock()
	delete(s.recordings, recording)
	unused := s.released && len(s.recordings) == 0
	s.encodeLocked()
	s.access.Unlock()
	if unused {
		s.close()
	}
}

func (s *screenStream) encodedInfo() screen.Info {
	if local, isLocal := s.device.(screen.LocalStream); isLocal {
		return local.EncodedInfo()
	}
	return s.info
}

func (s *screenStream) encodeLocked() {
	if local, isLocal := s.device.(screen.LocalStream); isLocal {
		local.Encode(len(s.recordings) > 0)
	}
}

func (s *screenStream) closeFrames() {
	s.decoding.Lock()
	defer s.decoding.Unlock()
	if s.frames != nil {
		s.frames.close()
		s.frames = nil
	}
}

func (s *screenStream) release() {
	s.access.Lock()
	s.released = true
	unused := len(s.recordings) == 0
	latest := s.takeLatestLocked()
	s.access.Unlock()
	if latest != nil {
		latest.release()
	}
	if unused {
		s.close()
		return
	}
	s.closeFrames()
}

func (s *screenStream) close() {
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		return
	}
	s.closed = true
	recordings := slices.Collect(maps.Keys(s.recordings))
	clear(s.recordings)
	latest := s.takeLatestLocked()
	s.access.Unlock()
	if latest != nil {
		latest.release()
	}
	close(s.done)
	<-s.finished
	s.closeFrames()
	s.device.Close()
	for _, recording := range recordings {
		recording.release()
	}
	s.engine.removeStream(s)
}
