package fault

import E "github.com/sagernet/sing/common/exceptions"

var (
	ErrUnavailable  = E.New("the device is unavailable")
	ErrDisconnected = E.New("the device is not connected")
	ErrInvalid      = E.New("invalid argument")
	ErrNotFound     = E.New("not found")
	ErrNotAnswering = E.New("not answering")
	ErrTimeout      = E.New("timed out")
)
