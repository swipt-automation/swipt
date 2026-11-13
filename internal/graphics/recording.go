//go:build cgo

package graphics

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/swipt-automation/swipt/internal/atomicfile"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/rfb/media"
)

const recordingManifestFile = "recording.json"

type recordingSegment struct {
	File     string  `json:"file"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
	Turns    int     `json:"turns"`
}

type recordingManifest struct {
	Start    float64            `json:"start"`
	Duration float64            `json:"duration"`
	Screen   device.ScreenInfo  `json:"screen"`
	Segments []recordingSegment `json:"segments"`
}

type recordingSession struct {
	directory string
	stream    *screenStream
	screen    device.ScreenInfo
	began     chan struct{}

	access   sync.Mutex
	origin   float64
	last     float64
	segments []recordingSegment
	writer   *movieWriter
	waiting  bool
	stopped  bool
	failure  error
}

func milliseconds(at time.Time) float64 {
	return float64(at.UnixNano()) / float64(time.Millisecond)
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
	if err = stream.addRecording(session); err != nil {
		return nil, err
	}
	stream.device.RequestKeyFrame()
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

func (r *recordingSession) append(unit *media.AccessUnit, format *hevcFormat, damaged bool) (needsKeyFrame bool) {
	r.access.Lock()
	defer r.access.Unlock()
	if r.stopped || r.failure != nil {
		return false
	}
	if damaged {
		r.waiting = true
		return true
	}
	if r.writer == nil || (format != nil && (r.writer.turns != unit.Turns || format != r.writer.format)) {
		if r.writer != nil {
			r.closeWriter()
			if r.failure != nil {
				return false
			}
		}
		if !unit.Keyframe() || format == nil {
			r.waiting = true
			return true
		}
		file := fmt.Sprint(len(r.segments)+1, ".mov")
		writer, err := newMovieWriter(filepath.Join(r.directory, file), format, unit)
		if err != nil {
			r.failLocked(err)
			return false
		}
		arrival := milliseconds(unit.Arrival)
		if len(r.segments) == 0 {
			r.origin = arrival
			close(r.began)
		}
		r.writer = writer
		r.segments = append(r.segments, recordingSegment{File: file, Start: arrival - r.origin, Turns: unit.Turns})
		r.waiting = false
	}
	if r.waiting {
		if !unit.Keyframe() {
			return true
		}
		r.waiting = false
	}
	r.writer.append(unit)
	r.last = milliseconds(unit.Arrival)
	if r.writer.failure != nil {
		r.closeWriter()
	}
	return false
}

func (r *recordingSession) fail(err error) {
	r.access.Lock()
	defer r.access.Unlock()
	r.failLocked(err)
}

func (r *recordingSession) failLocked(err error) {
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
		r.failLocked(err)
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
	duration := max(r.last-r.origin, 0) + float64(lastFrameDuration)/float64(time.Millisecond)
	manifest := recordingManifest{
		Start:    rounded(r.origin),
		Duration: rounded(duration),
		Screen:   r.screen,
		Segments: make([]recordingSegment, len(r.segments)),
	}
	for index, segment := range r.segments {
		following := duration
		if index+1 < len(r.segments) {
			following = r.segments[index+1].Start
		}
		manifest.Segments[index] = recordingSegment{File: segment.File, Start: rounded(segment.Start), Duration: rounded(following - segment.Start), Turns: segment.Turns}
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	if err = atomicfile.Write(filepath.Join(r.directory, recordingManifestFile), encoded, 0o644); err != nil {
		return err
	}
	return r.failure
}

func (r *recordingSession) release() {
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
		graphics := r.stream.engine.graphics
		message := automation.Message(err)
		graphics.logger.Error("save the abandoned recording: ", message)
		if graphics.onRecordingFailed != nil {
			graphics.onRecordingFailed(message)
		}
	}()
}

func readRecordingManifest(directory string) (*recordingManifest, error) {
	data, err := os.ReadFile(filepath.Join(directory, recordingManifestFile))
	if err != nil {
		return nil, failure(nameNotFound, "graphics: ", filepath.Base(directory), " has no ", recordingManifestFile)
	}
	var manifest recordingManifest
	if err = json.Unmarshal(data, &manifest); err != nil {
		return nil, failure(nameData, "graphics: read ", recordingManifestFile, ": ", err.Error())
	}
	if manifest.Duration < 0 {
		return nil, failure(nameData, "graphics: ", recordingManifestFile, " has a negative duration")
	}
	if manifest.Screen.Scale <= 0 {
		manifest.Screen.Scale = 1
	}
	var segments []recordingSegment
	for _, segment := range manifest.Segments {
		if segment.File == "" || segment.File == "." || segment.File == ".." || strings.ContainsAny(segment.File, `/\`) {
			continue
		}
		segment.Turns &= 3
		segments = append(segments, segment)
	}
	if len(segments) == 0 {
		return nil, failure(nameData, "graphics: ", recordingManifestFile, " lists no video")
	}
	manifest.Segments = segments
	return &manifest, nil
}

func (m *recordingManifest) info() string {
	encoded, _ := json.Marshal(map[string]float64{"start": rounded(m.Start), "duration": rounded(m.Duration)})
	return string(encoded)
}

func rounded(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0
	}
	return math.Round(value*100) / 100
}
