//go:build cgo && (darwin || android)

package graphics

import (
	"context"
	"path/filepath"
	"sync"
)

type movieReader interface {
	frame(ctx context.Context, seconds float64) (nativeFrame, error)
	close()
}

type screenRecording struct {
	directory string
	manifest  *recordingManifest

	access     sync.Mutex
	references int
	readers    map[int]movieReader
}

func newScreenRecording(directory string, manifest *recordingManifest) *screenRecording {
	return &screenRecording{directory: directory, manifest: manifest, references: 1, readers: make(map[int]movieReader)}
}

func (r *screenRecording) retain() *screenRecording {
	r.access.Lock()
	defer r.access.Unlock()
	r.references++
	return r
}

func (r *screenRecording) release() {
	r.access.Lock()
	r.references--
	var readers []movieReader
	if r.references == 0 {
		for _, reader := range r.readers {
			readers = append(readers, reader)
		}
		clear(r.readers)
	}
	r.access.Unlock()
	for _, reader := range readers {
		reader.close()
	}
}

func (r *screenRecording) reader(segment int) (movieReader, error) {
	r.access.Lock()
	defer r.access.Unlock()
	if found := r.readers[segment]; found != nil {
		return found, nil
	}
	opened, err := openMovie(filepath.Join(r.directory, r.manifest.Segments[segment].File))
	if err != nil {
		return nil, err
	}
	r.readers[segment] = opened
	return opened, nil
}

func (r *screenRecording) segmentAt(at float64) int {
	chosen := 0
	for index, segment := range r.manifest.Segments {
		if segment.Start <= at {
			chosen = index
		}
	}
	return chosen
}

type recordedSource struct {
	recording *screenRecording
	segment   int
	at        float64
}

func (s *recordedSource) render(ctx context.Context) (*raster, error) {
	reader, err := s.recording.reader(s.segment)
	if err != nil {
		return nil, err
	}
	segment := s.recording.manifest.Segments[s.segment]
	native, err := reader.frame(ctx, max(s.at-segment.Start, 0)/1000)
	if err != nil {
		return nil, err
	}
	defer native.release()
	screen := s.recording.manifest.Screen
	return (&frameSource{native: native, width: screen.Width, height: screen.Height, turns: segment.Turns}).render(ctx)
}

func (s *recordedSource) release() {
	s.recording.release()
}

func (r *screenRecording) frame(at float64) *imageNode {
	at = min(max(at, 0), r.manifest.Duration)
	segment := r.segmentAt(at)
	screen := r.manifest.Screen
	width, height := screen.Width, screen.Height
	if r.manifest.Segments[segment].Turns%2 == 1 {
		width, height = height, width
	}
	return newImageNode(width, height, scaling(1/screen.Scale, 1/screen.Scale), at, r.manifest.Start+at, &recordedSource{recording: r.retain(), segment: segment, at: at})
}
