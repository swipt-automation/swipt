//go:build cgo

package graphics

import (
	F "github.com/sagernet/sing/common/format"
	"github.com/swipt-automation/swipt/internal/automation"
)

const (
	nameGeneric      = "Error"
	nameType         = "TypeError"
	nameRange        = "RangeError"
	nameData         = "DataError"
	nameUnavailable  = "UnavailableError"
	nameState        = "InvalidStateError"
	nameNotFound     = "NotFoundError"
	nameNotSupported = "NotSupportedError"
)

const argumentsMalformed = "graphics: the arguments are not JSON"

func failure(name string, message ...any) error {
	return &automation.Failure{Name: name, Message: F.ToString(message...)}
}
