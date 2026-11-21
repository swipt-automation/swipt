//go:build android

package android

import (
	"slices"
	"strings"
)

var failureNames = []string{
	"TypeError", "NotFoundError", "NotAllowedError", "NotSupportedError", "InvalidStateError", "UnavailableError",
	"TimeoutError", "AbortError", "Error",
}

type Error struct {
	Name    string
	Message string
}

func (e *Error) Error() string {
	return e.Name + ": " + e.Message
}

func Failure(err error) error {
	if err == nil {
		return nil
	}
	name, message, found := strings.Cut(err.Error(), ": ")
	if !found || !slices.Contains(failureNames, name) {
		return err
	}
	return &Error{Name: name, Message: message}
}
