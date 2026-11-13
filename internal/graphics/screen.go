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
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/rfb/media"
)

const (
	screenOpenLimit       = 20 * time.Second
	screenFirstFrameLimit = 5 * time.Second
	screenBacklog         = 256
)

var (
	errScreenSilent = failure(nameUnavailable, "graphics: the screen sent no frame within ", int(screenFirstFrameLimit/time.Second), " s; it may be off")
	errScreenClosed = failure(nameState, "graphics: the screen stream is closed")
)

type frame interface {
	reference
	retain() frame
}

type decoder interface {
	decode(unit *media.AccessUnit, format *hevcFormat, formatChanged bool, damaged bool) (needsKeyFrame bool)
	close()
}

type screenStream struct {
	engine   *engine
	device   *device.ScreenStream
	info     device.ScreenInfo
	units    chan *media.AccessUnit
	dropped  atomic.Bool
	done     chan struct{}
	finished chan struct{}
	format   *hevcFormat

	decoding sync.Mutex
	decoder  decoder

	access     sync.Mutex
	recordings map[*recordingSession]struct{}
	latest     frame
	sequence   int64
	changed    chan struct{}
	failure    error
	released   bool
	closed     bool
}

func (e *engine) openScreen(ctx context.Context, options device.ScreenStreamOptions) (*screenStream, error) {
	if options.Mode == "" {
		options.Mode = device.ScreenModeRemoteDesktop
	}
	stream := &screenStream{
		engine:     e,
		units:      make(chan *media.AccessUnit, screenBacklog),
		done:       make(chan struct{}),
		finished:   make(chan struct{}),
		recordings: make(map[*recordingSession]struct{}),
		changed:    make(chan struct{}),
	}
	ctx, cancel := context.WithTimeout(ctx, screenOpenLimit)
	defer cancel()
	opened, err := e.graphics.connection.OpenScreenStream(ctx, options, stream)
	if err != nil {
		return nil, err
	}
	stream.device = opened
	stream.info = opened.Info()
	stream.decoder = newDecoder(stream)
	go stream.run()
	if !e.addStream(stream) {
		stream.close()
		return nil, automation.ErrSessionEnded
	}
	return stream, nil
}

func (s *screenStream) ScreenUnit(unit *media.AccessUnit) {
	select {
	case s.units <- unit:
	case <-s.done:
	default:
		s.dropped.Store(true)
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
		case unit := <-s.units:
			damaged := s.dropped.Swap(false) || unit.Damaged
			formatChanged := s.adoptFormat(unit)
			needsKeyFrame := false
			for _, recording := range s.activeRecordings() {
				needsKeyFrame = recording.append(unit, s.format, damaged) || needsKeyFrame
			}
			s.decoding.Lock()
			if s.decoder != nil {
				needsKeyFrame = s.decoder.decode(unit, s.format, formatChanged, damaged) || needsKeyFrame
			}
			s.decoding.Unlock()
			if needsKeyFrame {
				s.device.RequestKeyFrame()
			}
		}
	}
}

func (s *screenStream) adoptFormat(unit *media.AccessUnit) bool {
	vps, sps, pps := unit.ParameterSets()
	if vps == nil || sps == nil || pps == nil {
		return false
	}
	if s.format != nil && bytes.Equal(vps, s.format.vps) && bytes.Equal(sps, s.format.source) && bytes.Equal(pps, s.format.pps) {
		return false
	}
	format, err := newHEVCFormat(vps, sps, pps, s.info)
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

func (s *screenStream) takeLatestLocked() frame {
	latest := s.latest
	s.latest = nil
	close(s.changed)
	s.changed = make(chan struct{})
	return latest
}

func (s *screenStream) frameAfter(ctx context.Context, previous int64, limited bool) (frame, int64, error) {
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
	defer s.access.Unlock()
	switch {
	case s.closed:
		return errScreenClosed
	case s.failure != nil:
		return s.failure
	}
	s.recordings[recording] = struct{}{}
	return nil
}

func (s *screenStream) removeRecording(recording *recordingSession) {
	s.access.Lock()
	delete(s.recordings, recording)
	unused := s.released && len(s.recordings) == 0
	s.access.Unlock()
	if unused {
		s.close()
	}
}

func (s *screenStream) closeDecoder() {
	s.decoding.Lock()
	defer s.decoding.Unlock()
	if s.decoder != nil {
		s.decoder.close()
		s.decoder = nil
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
	s.closeDecoder()
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
	s.closeDecoder()
	s.device.Close()
	for _, recording := range recordings {
		recording.release()
	}
	s.engine.removeStream(s)
}
