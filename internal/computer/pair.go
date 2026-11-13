//go:build cgo

package computer

import (
	"context"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"
	"github.com/swipt-automation/swipt/internal/coredevice/pairing"
)

func (s *Session) Pair(ctx context.Context, bundleID, file string) error {
	if s.peer.network != nil {
		return E.New("pairing needs the device connected to this computer with a cable")
	}
	conn, err := s.service(ctx, lockdown.RemotePairing)
	if err != nil {
		return err
	}
	record, err := pairing.Setup(ctx, conn, pairing.SetupOptions{HostName: Name(), Logger: s.logger})
	if err != nil {
		return err
	}
	data, err := record.Marshal()
	if err != nil {
		return err
	}
	container, err := s.Container(ctx, bundleID)
	if err != nil {
		return err
	}
	defer container.Close()
	return container.Write(ctx, file, data)
}
