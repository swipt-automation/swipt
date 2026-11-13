//go:build cgo

package graphics

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/swipt-automation/swipt/internal/automation"
)

const recordingManifest = "recording.json"

type recordingSegment struct {
	File     string  `json:"file"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
	Turns    int     `json:"turns"`
}

type recordingScreen struct {
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Scale  float64 `json:"scale"`
}

type recordingMetadata struct {
	Start    float64            `json:"start"`
	Duration float64            `json:"duration"`
	Screen   recordingScreen    `json:"screen"`
	Segments []recordingSegment `json:"segments"`
}

type recordingSession struct {
	directory string
	stream    *screenStream
	access    sync.Mutex
	began     chan struct{}
	origin    float64
	last      float64
	segments  []recordingSegment
	writer    *movieWriter
	format    *hevcFormat
	waiting   bool
	stopped   bool
	failure   error
}

func recordScreen(call *automation.GraphicsCall, stream *screenStream, root, path string) (*recordingSession, error) {
	directory, err := call.Directory(root, path, true)
	if err != nil {
		return nil, err
	}
	existing, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	if len(existing) > 0 {
		return nil, failure(nameState, "graphics: ", filepath.Base(directory), " is not empty")
	}
	session := &recordingSession{directory: directory, stream: stream, began: make(chan struct{}), waiting: true}
	if !stream.addRecording(session) {
		return nil, errScreenClosed
	}
	stream.source.device.RequestKeyFrame()
	timer := time.NewTimer(screenFirstFrameLimit)
	defer timer.Stop()
	select {
	case <-session.began:
		session.access.Lock()
		if len(session.segments) == 0 {
			err = session.failure
		}
		session.access.Unlock()
		if err == nil {
			return session, nil
		}
	case <-timer.C:
		err = errScreenSilent
	case <-call.Context().Done():
		err = call.Context().Err()
	}
	_ = session.stop()
	return nil, err
}

func (r *recordingSession) append(unit *accessUnit) (needsKeyFrame bool) {
	r.access.Lock()
	defer r.access.Unlock()
	if r.stopped || r.failure != nil {
		return false
	}
	if unit.damaged {
		r.waiting = true
		return true
	}
	if unit.nal(nalSPS) != nil {
		if format, err := parseHEVCFormat(unit, r.stream.source.info.Width, r.stream.source.info.Height); err == nil {
			r.format = format
		}
	}
	turns := unit.turns()
	if r.writer == nil || (r.format != nil && (r.writer.turns != turns || !r.format.equal(r.writer.format))) {
		if r.writer != nil {
			r.closeWriter()
			if r.failure != nil {
				return false
			}
		}
		if !unit.keyframe() || r.format == nil {
			r.waiting = true
			return true
		}
		file := fmt.Sprint(len(r.segments)+1, ".mov")
		writer, err := newMovieWriter(filepath.Join(r.directory, file), r.format, turns)
		if err != nil {
			r.fail(err)
			return false
		}
		if len(r.segments) == 0 {
			r.origin = unit.arrival
			close(r.began)
		}
		r.writer = writer
		r.segments = append(r.segments, recordingSegment{File: file, Start: unit.arrival - r.origin, Turns: turns})
		r.waiting = false
	}
	if r.waiting {
		if !unit.keyframe() {
			return true
		}
		r.waiting = false
	}
	r.writer.append(unit)
	r.last = unit.arrival
	if r.writer.failure != nil {
		r.closeWriter()
	}
	return false
}

func (r *recordingSession) fail(err error) {
	if r.failure != nil {
		return
	}
	r.failure = err
	if len(r.segments) == 0 {
		close(r.began)
	}
}

func (r *recordingSession) closeWriter() {
	if err := r.writer.finish(); err != nil {
		r.fail(err)
	}
	r.writer = nil
}

func (r *recordingSession) stop() error {
	r.access.Lock()
	if r.stopped {
		r.access.Unlock()
		return failure(nameState, "graphics: the recording already stopped")
	}
	r.stopped = true
	r.access.Unlock()
	return r.finish()
}

func (r *recordingSession) finish() error {
	r.stream.removeRecording(r)
	r.access.Lock()
	defer r.access.Unlock()
	if r.writer != nil {
		r.closeWriter()
	}
	if len(r.segments) == 0 {
		if r.failure != nil {
			return r.failure
		}
		return failure(nameUnavailable, "graphics: the recording got no frames; the screen may be off or another app may hold it")
	}
	duration := max(r.last-r.origin, 0) + 1000.0/60
	metadata := recordingMetadata{
		Start:    rounded(r.origin),
		Duration: rounded(duration),
		Screen:   recordingScreen{Width: r.stream.source.info.Width, Height: r.stream.source.info.Height, Scale: r.stream.source.info.Scale},
		Segments: make([]recordingSegment, len(r.segments)),
	}
	for index, segment := range r.segments {
		following := duration
		if index+1 < len(r.segments) {
			following = r.segments[index+1].Start
		}
		metadata.Segments[index] = recordingSegment{File: segment.File, Start: rounded(segment.Start), Duration: rounded(following - segment.Start), Turns: segment.Turns}
	}
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	if err = writeAtomically(filepath.Join(r.directory, recordingManifest), encoded); err != nil {
		return err
	}
	return r.failure
}

func (r *recordingSession) abandon() {
	r.access.Lock()
	running := !r.stopped
	r.stopped = true
	r.access.Unlock()
	if !running {
		return
	}
	go func() {
		err := r.finish()
		if err == nil {
			return
		}
		options := current.Load().options
		message := automation.Message(err)
		options.Logger.Error("save the abandoned recording: ", message)
		if options.OnRecordingFailed != nil {
			options.OnRecordingFailed(message)
		}
	}()
}

func writeAtomically(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	staging, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	err = staging.Chmod(0o644)
	if err == nil {
		_, err = staging.Write(data)
	}
	if closeErr := staging.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(staging.Name(), path)
	}
	if err != nil {
		os.Remove(staging.Name())
	}
	return err
}

func rounded(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Round(value*100) / 100
}
