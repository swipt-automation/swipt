//go:build cgo

package graphics

import (
	F "github.com/sagernet/sing/common/format"
	"github.com/swipt-automation/swipt/internal/automation"
)

const (
	nameType         = "TypeError"
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

func unsupported(name string) error {
	return failure(nameNotSupported, "graphics: ", name, " is not available on this platform")
}
