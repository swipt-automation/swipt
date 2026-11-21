package api

import (
	"context"
	"errors"
	"io/fs"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func StatusError(err error) error {
	if err == nil {
		return nil
	}
	if _, isStatus := status.FromError(err); isStatus {
		return err
	}
	code := codes.Unknown
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		code = codes.DeadlineExceeded
	case errors.Is(err, context.Canceled):
		code = codes.Canceled
	case errors.Is(err, automation.ErrInvalid):
		code = codes.InvalidArgument
	case E.IsMulti(err, automation.ErrNotFound, fs.ErrNotExist):
		code = codes.NotFound
	case errors.Is(err, automation.ErrNotAllowed):
		code = codes.PermissionDenied
	case E.IsMulti(err, automation.ErrOutdated, automation.ErrSwipletOutdated), errors.As(err, new(*automation.IOSOutdatedError)):
		code = codes.FailedPrecondition
	}
	return status.Error(code, automation.Message(err))
}

func revocation(ctx context.Context) error {
	if cause := context.Cause(ctx); errors.Is(cause, errTokenReset) {
		return cause
	}
	return nil
}
