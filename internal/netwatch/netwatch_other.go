//go:build !(darwin || linux || windows)

package netwatch

import (
	"context"
	"os"
)

func Watch(context.Context) (<-chan struct{}, error) {
	return nil, os.ErrInvalid
}
