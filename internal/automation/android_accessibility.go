//go:build android && cgo

package automation

import (
	"context"
	"encoding/json"

	"github.com/sagernet/sing/common"
	"github.com/swipt-automation/swipt/internal/android"
	"github.com/swipt-automation/swipt/internal/jsc"
)

type androidSnapshot struct {
	*android.Snapshot
}

func (s androidSnapshot) search(engine *engine, scope string, encoded []byte, all bool, callback jsc.Value) (any, error) {
	found, err := search(s.Snapshot, android.Vocabulary, scope, encoded, all, scriptCallbacks[*android.Element]{
		engine: engine, callback: callback, argument: func(element *android.Element) (string, error) { return element.Handle, nil },
	})
	return common.Map(found, func(element *android.Element) string { return element.Handle }), err
}

func androidAccessibilitySnapshot(ctx context.Context, call *operation) (any, error) {
	var params struct {
		Display *int32 `json:"display"`
	}
	if err := call.androidParams(&params); err != nil {
		return nil, err
	}
	display := int32(-1)
	if params.Display != nil {
		display = *params.Display
	}
	encoded, err := call.android().AccessibilitySnapshot(ctx, display)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, androidError(err)
	}
	snapshot, err := android.ParseSnapshot([]byte(encoded))
	if err != nil {
		return nil, err
	}
	return struct {
		ID       string          `json:"id"`
		Snapshot json.RawMessage `json:"snapshot"`
	}{call.engine.snapshots.keep(androidSnapshot{snapshot}), json.RawMessage(encoded)}, nil
}
