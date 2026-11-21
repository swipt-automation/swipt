//go:build !android && cgo

package automation

import (
	"encoding/json"

	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/jsc"
)

type snapshot struct {
	ID string `json:"id"`
	*device.View
}

type iosSnapshot struct {
	*device.View
}

func (s iosSnapshot) search(engine *engine, scope string, encoded []byte, all bool, callback jsc.Value) (any, error) {
	return search(s.View, device.Vocabulary, scope, encoded, all, scriptCallbacks[*device.ViewElement]{engine: engine, callback: callback, argument: shallow})
}

func shallow(element *device.ViewElement) (string, error) {
	copied := *element
	copied.Children = nil
	data, err := json.Marshal(&copied)
	return string(data), err
}
