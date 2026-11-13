//go:build cgo

package graphics

/*
#include "graphics.h"
*/
import "C"

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"runtime/cgo"
	"slices"
	"strings"
	"sync"
	"unsafe"

	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
)

type reference interface {
	release()
}

type engine struct {
	graphics *Graphics

	access   sync.Mutex
	next     int64
	prepared map[int64]*call
	objects  map[int64]any
	streams  map[*screenStream]struct{}
	closed   bool
}

type call struct {
	method    string
	arguments json.RawMessage
	objects   []any
	owned     []reference
}

type callArguments struct {
	Root           string   `json:"root"`
	Path           string   `json:"path"`
	Mode           string   `json:"mode"`
	MaximumBitrate uint64   `json:"maximumBitrate"`
	Scale          *float64 `json:"scale"`
	Format         string   `json:"format"`
}

func newEngine(graphics *Graphics) *engine {
	return &engine{
		graphics: graphics,
		prepared: make(map[int64]*call),
		objects:  make(map[int64]any),
		streams:  make(map[*screenStream]struct{}),
	}
}

func release(object any) {
	if owned, isOwned := object.(reference); isOwned {
		owned.release()
	}
}

func (c *call) release() {
	for _, owned := range c.owned {
		owned.release()
	}
	c.owned = nil
}

func (c *call) first() any {
	if len(c.objects) == 0 {
		return nil
	}
	return c.objects[0]
}

func (c *call) stream() (*screenStream, error) {
	stream, isStream := c.first().(*screenStream)
	if !isStream {
		return nil, failure(nameType, "graphics: ", c.method, " needs a ScreenStream")
	}
	return stream, nil
}

func (e *engine) Run(identifier int64, context *automation.GraphicsCall) (json.RawMessage, error) {
	c := e.takePrepared(identifier)
	if c == nil {
		return nil, automation.ErrSessionEnded
	}
	value, err := e.run(c, context)
	c.release()
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func (e *engine) run(c *call, context *automation.GraphicsCall) (any, error) {
	var arguments callArguments
	if err := json.Unmarshal(c.arguments, &arguments); err != nil {
		return nil, failure(nameType, argumentsMalformed)
	}
	switch c.method {
	case "screen.open":
		stream, err := e.openScreen(context.Context(), device.ScreenStreamOptions{Mode: device.ScreenMode(arguments.Mode), MaximumBitrate: arguments.MaximumBitrate})
		if err != nil {
			return nil, err
		}
		return map[string]int64{"stream": e.register(stream)}, nil
	case "screen.close":
		stream, err := c.stream()
		if err != nil {
			return nil, err
		}
		stream.close()
		return struct{}{}, nil
	case "screen.frame":
		if !framesAvailable {
			return nil, unsupported("frames")
		}
		stream, err := c.stream()
		if err != nil {
			return nil, err
		}
		frame, _, err := stream.frameAfter(context.Context(), 0, true)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"image": e.register(frame)}, nil
	case "screen.record":
		stream, err := c.stream()
		if err != nil {
			return nil, err
		}
		session, err := recordScreen(context, stream, arguments.Root, arguments.Path)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"session": e.register(session)}, nil
	case "recording.stop":
		session, isSession := c.first().(*recordingSession)
		if !isSession {
			return nil, failure(nameType, "graphics: stop needs a RecordingSession")
		}
		return struct{}{}, session.stop()
	case "recording.open":
		directory, err := context.Directory(arguments.Root, arguments.Path, false)
		if err != nil {
			return nil, err
		}
		manifest, err := readRecordingManifest(directory)
		if err != nil {
			return nil, err
		}
		recording, err := newScreenRecording(directory, manifest)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"recording": e.register(recording)}, nil
	case "recording.export":
		recording, isRecording := c.first().(*screenRecording)
		if !isRecording {
			return nil, failure(nameType, "graphics: ", c.method, " needs a ScreenRecording")
		}
		extension := strings.ToLower(filepath.Ext(arguments.Path))
		if extension != ".mov" && extension != ".mp4" {
			return nil, failure(nameType, "graphics: export to a .mov or .mp4 file")
		}
		staged, err := context.StageFile(arguments.Root, arguments.Path)
		if err != nil {
			return nil, err
		}
		if err = recording.export(context, staged.Path(), extension == ".mp4"); err != nil {
			staged.Discard()
			return nil, err
		}
		return struct{}{}, staged.Commit()
	}
	return e.runNative(c, arguments, context)
}

func (e *engine) Watch(identifier int64, context *automation.GraphicsCall, publish func(any)) error {
	c := e.takePrepared(identifier)
	if c == nil {
		return automation.ErrSessionEnded
	}
	defer c.release()
	if c.method != "screen.frames" {
		return failure(nameType, "graphics: there is no stream ", c.method)
	}
	if !framesAvailable {
		return unsupported("frames")
	}
	stream, err := c.stream()
	if err != nil {
		return err
	}
	var sequence, previous int64
	defer func() {
		release(e.take(previous))
	}()
	for {
		var next frame
		next, sequence, err = stream.frameAfter(context.Context(), sequence, false)
		if err != nil {
			return err
		}
		identifier := e.register(next)
		release(e.take(previous))
		previous = identifier
		publish(identifier)
	}
}

func (e *engine) Close() {
	e.access.Lock()
	e.closed = true
	objects := slices.Collect(maps.Values(e.objects))
	prepared := slices.Collect(maps.Values(e.prepared))
	streams := slices.Collect(maps.Keys(e.streams))
	clear(e.objects)
	clear(e.prepared)
	clear(e.streams)
	e.access.Unlock()
	go func() {
		for _, object := range objects {
			release(object)
		}
		for _, c := range prepared {
			c.release()
		}
		for _, stream := range streams {
			stream.close()
		}
	}()
}

func (e *engine) takePrepared(identifier int64) *call {
	e.access.Lock()
	defer e.access.Unlock()
	c := e.prepared[identifier]
	delete(e.prepared, identifier)
	return c
}

func (e *engine) register(object any) int64 {
	e.access.Lock()
	e.next++
	identifier := e.next
	accepted := !e.closed
	if accepted {
		e.objects[identifier] = object
	}
	e.access.Unlock()
	if !accepted {
		go release(object)
	}
	return identifier
}

func (e *engine) take(identifier int64) any {
	e.access.Lock()
	defer e.access.Unlock()
	object := e.objects[identifier]
	delete(e.objects, identifier)
	return object
}

func (e *engine) addStream(stream *screenStream) bool {
	e.access.Lock()
	defer e.access.Unlock()
	if e.closed {
		return false
	}
	e.streams[stream] = struct{}{}
	return true
}

func (e *engine) removeStream(stream *screenStream) {
	e.access.Lock()
	defer e.access.Unlock()
	delete(e.streams, stream)
}

func engineOf(handle C.uintptr_t) *engine {
	return cgo.Handle(handle).Value().(*engine)
}

func setFailure(out *C.swipt_graphics_failure, err error) {
	out.name = C.CString(automation.FailureName(err))
	out.message = C.CString(automation.Message(err))
}

//export swiptGraphicsPrepare
func swiptGraphicsPrepare(handle C.uintptr_t, method *C.char, arguments *C.char, objects *C.swipt_graphics_object, count C.size_t, out *C.swipt_graphics_failure) C.int64_t {
	e := engineOf(handle)
	c := &call{method: C.GoString(method), arguments: json.RawMessage(C.GoString(arguments))}
	var err error
	for index, object := range unsafe.Slice(objects, int(count)) {
		value, owned, known := objectOf(object)
		if owned != nil {
			c.owned = append(c.owned, owned)
		}
		if !known && err == nil {
			err = failure(nameType, "graphics: argument ", index+1, " of ", c.method, " is not an Image, a ScreenStream, a RecordingSession, a ScreenRecording or a Uint8Array")
		}
		c.objects = append(c.objects, value)
	}
	if err == nil && !json.Valid(c.arguments) {
		err = failure(nameType, argumentsMalformed)
	}
	e.access.Lock()
	if err == nil && e.closed {
		err = automation.ErrSessionEnded
	}
	if err == nil {
		e.next++
		e.prepared[e.next] = c
	}
	identifier := e.next
	e.access.Unlock()
	if err != nil {
		c.release()
		setFailure(out, err)
		return 0
	}
	return C.int64_t(identifier)
}

//export swiptGraphicsTake
func swiptGraphicsTake(handle C.uintptr_t, identifier C.int64_t) C.swipt_graphics_taken {
	object := engineOf(handle).take(int64(identifier))
	switch object.(type) {
	case nil:
		return C.swipt_graphics_taken{}
	case *screenStream, *recordingSession:
		return C.swipt_graphics_taken{kind: C.SWIPT_GRAPHICS_HANDLE, handle: C.uintptr_t(cgo.NewHandle(object))}
	}
	return takenOf(object)
}

//export swiptGraphicsRelease
func swiptGraphicsRelease(handle C.uintptr_t) {
	value := cgo.Handle(handle)
	object := value.Value()
	value.Delete()
	go release(object)
}

//export swiptGraphicsDeleteHandle
func swiptGraphicsDeleteHandle(handle C.uintptr_t) {
	cgo.Handle(handle).Delete()
}
