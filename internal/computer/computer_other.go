//go:build cgo && !darwin

package computer

import (
	"context"
	"os"
	"strings"

	"github.com/swipt-automation/swipt/internal/api/discovery"
	"github.com/swipt-automation/swipt/internal/mdns"
)

const systemDiskImages = ""

func Name() string {
	name, _ := os.Hostname()
	return name
}

func instanceName() string {
	name, _, _ := strings.Cut(Name(), ".")
	return name
}

func advertise(ctx context.Context, port uint16, udid string) (string, func(), error) {
	advertisement, err := mdns.Advertise(ctx, instanceName(), discovery.ServiceType, label(instanceName()+" swipt"), port, map[string]string{discovery.UDIDKey: udid})
	if err != nil {
		return "", nil, err
	}
	return advertisement.Host(), advertisement.Close, nil
}
