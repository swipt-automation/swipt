//go:build cgo

package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
)

type subscriptionFunc func(ctx context.Context, call *operation, publish func(any), ended func(error)) error

var subscriptions map[string]subscriptionFunc

func init() {
	subscriptions = map[string]subscriptionFunc{
		"screen.frames":  watchGraphics,
		"sqlite.changes": watchDatabaseChanges,
	}
}

func (e *engine) watch(ctx context.Context, kind string, args []json.RawMessage, publish func(any, error)) error {
	watch, found := subscriptions[kind]
	if !found {
		watch, found = deviceSubscriptions[kind]
	}
	if !found {
		return E.Cause1(ErrInvalid, E.New("unknown subscription ", strconv.Quote(kind)))
	}
	call := &operation{engine: e, name: kind, args: args}
	return watch(ctx, call, func(value any) { publish(value, nil) }, func(err error) {
		if ctx.Err() != nil {
			return
		}
		if errors.Is(err, context.Canceled) {
			err = call.droppedError(err, "while watching "+kind)
		}
		publish(nil, err)
	})
}

func watchDatabaseChanges(ctx context.Context, call *operation, publish func(any), _ func(error)) error {
	if err := call.decode(0); err != nil {
		return err
	}
	go call.engine.host.watchDatabaseChanges(ctx, call.engine.dataOwner(), publish)
	return nil
}
