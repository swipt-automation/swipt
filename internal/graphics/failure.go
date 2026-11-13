//go:build cgo

package graphics

import (
	F "github.com/sagernet/sing/common/format"
	"github.com/swipt-automation/swipt/internal/automation"
)

const (
	nameError        = "Error"
	nameType         = "TypeError"
	nameData         = "DataError"
	nameUnavailable  = "UnavailableError"
	nameState        = "InvalidStateError"
	nameAbort        = "AbortError"
	nameNotFound     = "NotFoundError"
	nameNotSupported = "NotSupportedError"
)

func failure(name string, message ...any) error {
	return &automation.Failure{Name: name, Message: F.ToString(message...)}
}
