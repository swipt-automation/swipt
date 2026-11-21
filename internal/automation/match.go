//go:build cgo

package automation

import (
	"errors"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/jsc"
	"github.com/swipt-automation/swipt/internal/selector"
)

var errThrown = E.New("the selector function threw")

type scriptCallbacks[T any] struct {
	engine   *engine
	callback jsc.Value
	argument func(T) (string, error)
}

func (c scriptCallbacks[T]) Predicate(id int, element T) (bool, error) {
	argument, err := c.argument(element)
	if err != nil {
		return false, err
	}
	return c.call("predicate", id, argument)
}

func (c scriptCallbacks[T]) Match(id int, text string) (bool, error) {
	return c.call("match", id, text)
}

func (c scriptCallbacks[T]) call(kind string, id int, value string) (bool, error) {
	arguments := []jsc.Value{c.engine.context.NewString(kind), c.engine.context.NewString(strconv.Itoa(id)), c.engine.context.NewString(value)}
	defer func() {
		for _, argument := range arguments {
			argument.Release()
		}
	}()
	result, err := c.callback.Call(arguments...)
	if err != nil {
		return false, err
	}
	defer result.Release()
	answer, err := result.ToString()
	if err != nil {
		return false, err
	}
	if answer == "throw" {
		return false, errThrown
	}
	return answer == "true", nil
}

type tree[T comparable] interface {
	Domain(scope string) (domain, universe []T, err error)
}

func search[T comparable](snapshot tree[T], vocabulary *selector.Vocabulary[T], scope string, encoded []byte, all bool, callbacks selector.Callbacks[T]) ([]T, error) {
	parsed, err := selector.Parse(vocabulary, encoded)
	if err != nil {
		return nil, err
	}
	domain, universe, err := snapshot.Domain(scope)
	if err != nil {
		return nil, err
	}
	if all {
		return parsed.FindAll(domain, universe, callbacks)
	}
	var none T
	element, err := parsed.Find(domain, universe, callbacks)
	if element == none {
		return nil, err
	}
	return []T{element}, err
}

func (e *engine) dropSnapshot(arguments []jsc.Value) (any, error) {
	values, err := texts(arguments, 1)
	if err != nil {
		return nil, err
	}
	e.snapshots.drop(values[0])
	return nil, nil
}

func (e *engine) match(arguments []jsc.Value) (any, error) {
	values, err := texts(arguments, 4)
	if err != nil {
		return nil, err
	}
	if len(arguments) < 5 {
		return nil, E.New("expected the selector callback")
	}
	id, scope, encoded, mode := values[0], values[1], values[2], values[3]
	snapshot, err := e.snapshots.get(id)
	if err != nil {
		return map[string]string{"failure": FailureOf(err)}, nil
	}
	found, err := snapshot.search(e, scope, []byte(encoded), mode == "all", arguments[4])
	switch {
	case errors.Is(err, errThrown):
		return map[string]bool{"thrown": true}, nil
	case err != nil:
		return map[string]string{"failure": FailureOf(err)}, nil
	}
	return map[string]any{"elements": found}, nil
}
