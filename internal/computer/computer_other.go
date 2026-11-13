//go:build cgo && !darwin

package computer

import (
	"context"
	"os"
	"strings"
	"unicode"

	"github.com/swipt-automation/swipt/internal/api/discovery"
	"github.com/swipt-automation/swipt/internal/mdns"
)

var systemDiskImages []string

func Name() string {
	name, _ := os.Hostname()
	return name
}

func advertise(ctx context.Context, name string, port uint16, txt map[string]string) (string, func(), error) {
	computer, _, _ := strings.Cut(Name(), ".")
	advertisement, err := mdns.Advertise(ctx, strings.ReplaceAll(name, ".", " "), discovery.ServiceType, label(computer+" swipt"), port, txt)
	if err != nil {
		return "", nil, err
	}
	return advertisement.Host(), advertisement.Close, nil
}

func label(name string) string {
	var parts []string
	var part strings.Builder
	for _, character := range strings.NewReplacer("'", "", "’", "").Replace(name) {
		if character < unicode.MaxASCII && (unicode.IsLetter(character) || unicode.IsDigit(character)) {
			part.WriteRune(character)
			continue
		}
		if part.Len() > 0 {
			parts = append(parts, part.String())
			part.Reset()
		}
	}
	if part.Len() > 0 {
		parts = append(parts, part.String())
	}
	return strings.Join(parts, "-")
}
