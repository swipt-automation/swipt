//go:build cgo

package computer

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"
	"github.com/swipt-automation/swipt/internal/coredevice/pairing"
	"github.com/swipt-automation/swipt/internal/log"
)

type PairOptions struct {
	Device string
	Logs   *log.Factory
}

func Pair(ctx context.Context, options PairOptions) ([]byte, error) {
	paired, err := chooseDevice(ctx, options.Device)
	if err != nil {
		return nil, err
	}
	logger := options.Logs.NewLogger("pairing")
	session, err := OpenSession(ctx, paired.UDID, logger)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	if session.peer.network != nil {
		return nil, E.New("pairing needs ", paired.Name, " connected to this computer with a cable")
	}
	conn, err := session.service(ctx, lockdown.RemotePairing)
	if err != nil {
		return nil, err
	}
	record, err := pairing.Setup(ctx, conn, pairing.SetupOptions{HostName: Name(), Logger: logger})
	if err != nil {
		return nil, err
	}
	return record.Marshal()
}
