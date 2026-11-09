package libSwipt

import (
	"bytes"
	"encoding/json"

	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/jsc"
)

type SwipletWebValue struct {
	Data    []byte
	IsData  bool
	JSON    string
	Failure string
}

type SwipletWebCompletion interface {
	Done(value *SwipletWebValue)
}

type SwipletWeb struct {
	session *automation.WebSession
}

func (p *Platform) OpenSwipletWeb(swiplet string) (*SwipletWeb, error) {
	session, err := p.host.OpenWeb(swiplet)
	if err != nil {
		return nil, err
	}
	return &SwipletWeb{session: session}, nil
}

func webValue(value any, err error) *SwipletWebValue {
	if err != nil {
		return &SwipletWebValue{Failure: automation.FailureOf(err)}
	}
	if data, isData := value.(jsc.Bytes); isData {
		return &SwipletWebValue{Data: data, IsData: true}
	}
	if value == nil {
		return &SwipletWebValue{JSON: "null"}
	}
	data, err := json.Marshal(value)
	if err != nil {
		return &SwipletWebValue{Failure: automation.FailureOf(err)}
	}
	return &SwipletWebValue{JSON: string(data)}
}

func (w *SwipletWeb) Start(id, name, arguments string, completion SwipletWebCompletion) {
	w.session.Start(id, name, arguments, func(value any, err error) {
		completion.Done(webValue(value, err))
	})
}

func (w *SwipletWeb) Cancel(id string) {
	w.session.Cancel(id)
}

func (w *SwipletWeb) Put(data []byte) string {
	return w.session.Put(bytes.Clone(data))
}

func (w *SwipletWeb) Release(id, reason string) {
	w.session.Release(id, reason)
}

func (w *SwipletWeb) OpenFile(root, path string) string {
	return w.session.OpenFile(root, path)
}

func (w *SwipletWeb) Compression(format, mode string) (StringIterator, error) {
	ids, err := w.session.Compression(format, mode)
	if err != nil {
		return nil, err
	}
	return newIterator(ids), nil
}

func (w *SwipletWeb) Random(length int32) (*SwipletWebValue, error) {
	data, err := w.session.Random(int(length))
	if err != nil {
		return nil, err
	}
	return &SwipletWebValue{Data: data, IsData: true}, nil
}

func (w *SwipletWeb) Digest(algorithm string, data []byte) (*SwipletWebValue, error) {
	sum, err := w.session.Digest(algorithm, data)
	if err != nil {
		return nil, err
	}
	return &SwipletWebValue{Data: sum, IsData: true}, nil
}

type SwipletDecodedText struct {
	Text     string
	Valid    bool
	Restored int32
}

func (w *SwipletWeb) DecodeText(data []byte, fatal bool) *SwipletDecodedText {
	text, restored, ok := automation.DecodeText(data, fatal)
	return &SwipletDecodedText{Text: text, Valid: ok, Restored: int32(restored)}
}

func (w *SwipletWeb) Close() {
	w.session.Close()
}
