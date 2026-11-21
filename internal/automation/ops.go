//go:build cgo

package automation

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/fault"
	"github.com/swipt-automation/swipt/internal/jsc"
)

type operation struct {
	engine     *engine
	name       string
	args       []json.RawMessage
	connection connectionWatch
}

type operationFunc func(ctx context.Context, call *operation) (any, error)

var operations map[string]operationFunc

func init() {
	operations = map[string]operationFunc{
		"fs.stat":           fsStat,
		"fs.list":           fsList,
		"fs.read":           fsRead,
		"fs.write":          fsWrite,
		"fs.mkdir":          fsMkdir,
		"fs.remove":         fsRemove,
		"fs.copy":           fsTransfer(false),
		"fs.move":           fsTransfer(true),
		"fs.writer":         fsWriter,
		"stream.read":       streamRead,
		"stream.readAll":    streamReadAll,
		"stream.write":      streamWrite,
		"stream.close":      streamClose,
		"stream.pipe":       streamPipe,
		"stream.create":     streamCreate,
		"fetch":             fetch,
		"cookies.get":       cookiesGet,
		"cookies.set":       cookiesSet,
		"cookies.clear":     cookiesClear,
		"websocket.connect": webSocketConnect,
		"websocket.read":    webSocketRead,
		"websocket.send":    webSocketSend,
		"websocket.close":   webSocketClose,
		"graphics":          graphicsRun,
		"crypto.encrypt":    cryptoEncrypt,
		"crypto.decrypt":    cryptoDecrypt,
		"crypto.sign":       cryptoSign,
		"crypto.verify":     cryptoVerify,
		"crypto.derive":     cryptoDerive,
		"crypto.generate":   cryptoGenerate,
		"crypto.import":     cryptoImport,
		"crypto.export":     cryptoExport,
		"sqlite":            sqliteOperation,
	}
}

func (o *operation) decode(required int, values ...any) error {
	args := o.args
	if len(args) > len(values) {
		return E.Cause1(ErrInvalid, E.New("takes ", len(values), " arguments, not ", len(args)))
	}
	for index, value := range values {
		if index >= len(args) || string(args[index]) == "null" {
			if index < required {
				if numeric(reflect.TypeOf(value)) {
					return E.Cause1(ErrInvalid, E.New("argument ", index+1, " is missing or not a finite number"))
				}
				return E.Cause1(ErrInvalid, E.New("argument ", index+1, " is missing"))
			}
			continue
		}
		if err := json.Unmarshal(args[index], value); err != nil {
			return E.Cause1(ErrInvalid, E.Cause(err, "argument ", index+1))
		}
	}
	return nil
}

func (o *operation) dropped(ctx context.Context, err error, since time.Time) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}
	if _, scripted := errors.AsType[*Failure](err); scripted {
		return false
	}
	return errors.Is(err, context.Canceled) || o.connection.interrupted(since)
}

func (o *operation) droppedError(err error, doing string) error {
	if cause := o.connection.dropCause(); cause != nil {
		err = cause
	}
	if errors.Is(err, context.Canceled) {
		return E.Extend(fault.ErrDisconnected, "the connection dropped ", doing)
	}
	return E.Extend(fault.ErrDisconnected, "the connection dropped ", doing, ": ", err)
}

var terseErrors = []error{ErrInvalid, ErrNotFound, ErrNotAllowed, errNetwork, fault.ErrTimeout, fault.ErrNotAnswering}

func Message(err error) string {
	message := err.Error()
	for _, sentinel := range terseErrors {
		if !errors.Is(err, sentinel) {
			continue
		}
		prefix := sentinel.Error() + ": "
		for strings.HasPrefix(message, prefix) {
			message = message[len(prefix):]
		}
		message = strings.ReplaceAll(message, ": "+prefix, ": ")
	}
	return message
}

type failureKind struct {
	name   string
	errors []error
}

var failureKinds = []failureKind{
	{"UnavailableError", []error{fault.ErrUnavailable, fault.ErrDisconnected, fault.ErrNotAnswering}},
	{"TimeoutError", []error{context.DeadlineExceeded, fault.ErrTimeout}},
	{"AbortError", []error{context.Canceled, errReset, ErrSessionEnded}},
	{"TypeError", []error{ErrInvalid, errNetwork}},
	{"NotFoundError", []error{ErrNotFound}},
	{"NotAllowedError", []error{ErrNotAllowed}},
}

func FailureName(err error) string {
	if scripted, isScripted := errors.AsType[*Failure](err); isScripted && scripted.Name != "" {
		return scripted.Name
	}
	for _, kind := range failureKinds {
		if E.IsMulti(err, kind.errors...) || E.IsMulti(err, deviceFailures[kind.name]...) {
			return kind.name
		}
	}
	return "Error"
}

func FailureOf(err error) string {
	data, _ := json.Marshal(struct {
		Name    string `json:"name"`
		Message string `json:"message"`
	}{FailureName(err), Message(err)})
	return string(data)
}

type Frame = jsc.Frame

type Failure struct {
	Name    string  `json:"name"`
	Message string  `json:"message"`
	Stack   []Frame `json:"stack"`
}

func (f *Failure) Error() string {
	return f.Message
}

func (f *Failure) Summary() string {
	text := f.Name + ": " + f.Message
	if len(f.Stack) > 0 {
		frame := f.Stack[0]
		text += " (" + frame.File + ":" + strconv.Itoa(frame.Line) + ":" + strconv.Itoa(frame.Column) + ")"
	}
	return text
}

func (f *Failure) Describe() string {
	var text strings.Builder
	text.WriteString(f.Name + ": " + f.Message)
	for _, frame := range f.Stack {
		position := frame.File + ":" + strconv.Itoa(frame.Line) + ":" + strconv.Itoa(frame.Column)
		if frame.Function != "" {
			position = frame.Function + " (" + position + ")"
		}
		text.WriteString("\n    at " + position)
	}
	return text.String()
}

func (f *Failure) Is(target error) bool {
	index := slices.IndexFunc(failureKinds, func(kind failureKind) bool { return kind.name == f.Name })
	return index >= 0 && target == failureKinds[index].errors[0]
}

func failureError(text string) error {
	failure := &Failure{}
	if json.Unmarshal([]byte(text), failure) != nil {
		return &Failure{Name: "Error", Message: text}
	}
	return failure
}

func numeric(value reflect.Type) bool {
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	return value.Kind() >= reflect.Int && value.Kind() <= reflect.Float64
}
