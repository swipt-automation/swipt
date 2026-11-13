//go:build cgo

package computer

import (
	"context"
	"slices"

	"github.com/swipt-automation/swipt/internal/coredevice/imagemounter"
	"github.com/swipt-automation/swipt/internal/log"
)

func diskImageDirectory(ctx context.Context, given []string, logger log.ContextLogger) (string, error) {
	directory, err := imagemounter.Newest(ctx, append(slices.Clip(given), systemDiskImages...))
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		logger.Debug("find a Developer Disk Image: ", err)
	}
	return directory, nil
}
