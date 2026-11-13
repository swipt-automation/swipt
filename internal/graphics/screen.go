//go:build cgo

package graphics

/*
#include "screen.h"
*/
import "C"

import (
	"context"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/swipt-automation/swipt/internal/device"
)

const (
	screenOpenLimit       = 20 * time.Second
	screenFirstFrameLimit = C.SWIPT_GRAPHICS_FIRST_FRAME_SECONDS * time.Second
	screenBacklog         = 256
)

var (
	errScreenSilent = failure(nameUnavailable, "graphics: the screen sent no frame within ", C.SWIPT_GRAPHICS_FIRST_FRAME_SECONDS, " s; it may be off")
	errScreenClosed = failure(nameState, "graphics: the screen stream is closed")
)

type screenObserver interface {
	screenUnit(unit *accessUnit) (needsKeyFrame bool)
	screenEnded(err error)
	screenClosed()
}

type screenSource struct {
	device   *device.ScreenStream
	info     device.ScreenInfo
	observer screenObserver
	batches  chan []byte
	done     chan struct{}
	finished chan struct{}

	access  sync.Mutex
	streams map[*screenStream]device.ScreenStreamOptions
	options device.ScreenStreamOptions
	failure error
	closed  bool
}

type screenStream struct {
	source *screenSource
	owner  *screenOwner

	access     sync.Mutex
	recordings map[*recordingSession]struct{}
	released   bool
	closed     bool
}

type screenOwner struct {
	access  sync.Mutex
	streams map[*screenStream]struct{}
	closed  bool
}

func newScreenOwner() *screenOwner {
	return &screenOwner{streams: make(map[*screenStream]struct{})}
}

func openScreen(ctx context.Context, owner *screenOwner, options device.ScreenStreamOptions) (*screenStream, error) {
	if options.Mode == "" {
		options.Mode = device.ScreenModeRemoteDesktop
	}
	stream := &screenStream{owner: owner, recordings: make(map[*recordingSession]struct{})}
	graphics := current.Load()
	graphics.screenAccess.Lock()
	defer graphics.screenAccess.Unlock()
	if source := graphics.screenSource; source != nil && source.join(stream, options) {
		stream.source = source
		return stream, nil
	}
	source, err := openScreenSource(ctx, graphics.options.Connection, stream, options)
	if err != nil {
		return nil, err
	}
	graphics.screenSource = source
	stream.source = source
	return stream, nil
}

func openScreenSource(ctx context.Context, connection *device.Connection, stream *screenStream, options device.ScreenStreamOptions) (*screenSource, error) {
	ctx, cancel := context.WithTimeout(ctx, screenOpenLimit)
	defer cancel()
	source := &screenSource{
		batches:  make(chan []byte, screenBacklog),
		done:     make(chan struct{}),
		finished: make(chan struct{}),
		streams:  map[*screenStream]device.ScreenStreamOptions{stream: options},
		options:  device.MergeScreenStreamOptions([]device.ScreenStreamOptions{options}),
	}
	opened, err := connection.OpenScreenStream(ctx, source.options, source.deliver, source.end)
	if err != nil {
		return nil, err
	}
	source.device = opened
	source.info = opened.Info()
	source.access.Lock()
	source.observer = newScreenObserver(source.info)
	if source.failure != nil {
		source.observer.screenEnded(source.failure)
	}
	source.access.Unlock()
	go source.run()
	return source, nil
}

func (s *screenSource) join(stream *screenStream, options device.ScreenStreamOptions) bool {
	s.access.Lock()
	defer s.access.Unlock()
	if s.closed || s.failure != nil {
		return false
	}
	s.streams[stream] = options
	s.configureLocked()
	s.device.RequestKeyFrame()
	return true
}

func (s *screenSource) leave(stream *screenStream) {
	s.access.Lock()
	delete(s.streams, stream)
	unused := len(s.streams) == 0
	if !unused {
		s.configureLocked()
	}
	s.access.Unlock()
	if unused {
		s.close()
	}
}

func (s *screenSource) configureLocked() {
	merged := device.MergeScreenStreamOptions(slices.Collect(maps.Values(s.streams)))
	if merged != s.options {
		s.options = merged
		s.device.Configure(merged)
	}
}

func (s *screenSource) deliver(batch []byte) {
	select {
	case s.batches <- batch:
	case <-s.done:
	default:
	}
}

func (s *screenSource) end(err error) {
	s.access.Lock()
	defer s.access.Unlock()
	if s.closed {
		return
	}
	s.failure = err
	if s.observer != nil {
		s.observer.screenEnded(err)
	}
}

func (s *screenSource) run() {
	defer close(s.finished)
	var units depacketizer
	for {
		select {
		case <-s.done:
			return
		case batch := <-s.batches:
			recordings, observed := s.consumers()
			var needsKeyFrame bool
			for _, unit := range units.depacketize(batch) {
				for _, recording := range recordings {
					needsKeyFrame = recording.append(unit) || needsKeyFrame
				}
				if observed {
					needsKeyFrame = s.observer.screenUnit(unit) || needsKeyFrame
				}
			}
			if needsKeyFrame {
				s.device.RequestKeyFrame()
			}
		}
	}
}

func (s *screenSource) consumers() (recordings []*recordingSession, observed bool) {
	s.access.Lock()
	defer s.access.Unlock()
	for stream := range s.streams {
		stream.access.Lock()
		recordings = slices.AppendSeq(recordings, maps.Keys(stream.recordings))
		observed = observed || !stream.released
		stream.access.Unlock()
	}
	return recordings, observed
}

func (s *screenSource) close() {
	s.access.Lock()
	if s.closed {
		s.access.Unlock()
		return
	}
	s.closed = true
	s.access.Unlock()
	close(s.done)
	<-s.finished
	s.device.Close()
	s.observer.screenClosed()
}

func (s *screenStream) addRecording(recording *recordingSession) bool {
	s.access.Lock()
	defer s.access.Unlock()
	if s.closed {
		return false
	}
	s.recordings[recording] = struct{}{}
	return true
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

func (s *screenStream) release() {
	s.access.Lock()
	s.released = true
	unused := len(s.recordings) == 0
	s.access.Unlock()
	if unused {
		s.close()
	}
}

func (s *screenStream) isClosed() bool {
	s.access.Lock()
	defer s.access.Unlock()
	return s.closed
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
	s.access.Unlock()
	for _, recording := range recordings {
		recording.abandon()
	}
	s.source.leave(s)
	s.owner.remove(s)
}

func (o *screenOwner) add(stream *screenStream) error {
	o.access.Lock()
	added := !o.closed
	if added {
		o.streams[stream] = struct{}{}
	}
	o.access.Unlock()
	if !added {
		stream.close()
		return failure(nameAbort, "the JavaScript session ended")
	}
	return nil
}

func (o *screenOwner) remove(stream *screenStream) {
	o.access.Lock()
	delete(o.streams, stream)
	o.access.Unlock()
}

func (o *screenOwner) close() {
	o.access.Lock()
	o.closed = true
	streams := slices.Collect(maps.Keys(o.streams))
	clear(o.streams)
	o.access.Unlock()
	for _, stream := range streams {
		stream.close()
	}
}

func release(object any) {
	switch owned := object.(type) {
	case *screenStream:
		owned.release()
	case *recordingSession:
		owned.abandon()
	}
}
