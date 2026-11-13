//go:build cgo

package computer

import (
	"context"
	"slices"

	"github.com/swipt-automation/swipt/internal/coredevice/imagemounter"
	"github.com/swipt-automation/swipt/internal/log"
)

// diskImageDirectory picks the newest Developer Disk Image among the given
// directories and, on macOS, the one Xcode installs.
func diskImageDirectory(ctx context.Context, given []string, logger log.ContextLogger) (string, error) {
	candidates := given
	if systemDiskImages != "" {
		candidates = append(slices.Clip(candidates), systemDiskImages)
	}
	directory, err := imagemounter.Newest(ctx, candidates)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		logger.Debug("find a Developer Disk Image: ", err)
	}
	return directory, nil
}
