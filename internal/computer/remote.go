//go:build cgo

package computer

import (
	"context"
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/swipt-automation/swipt/internal/atomicfile"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"
	"github.com/swipt-automation/swipt/internal/coredevice/pairing"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/mdns"
	"github.com/swipt-automation/swipt/internal/usbmuxd"
)

const (
	remotePairingFolder  = "RemotePairing"
	remotePairingSuffix  = ".plist"
	remotePairingService = "_remotepairing._tcp"
	browseTimeout        = 2 * time.Second
)

type networkDevice struct {
	udid      string
	record    *pairing.Record
	addresses []netip.AddrPort
}

func (d networkDevice) connect(ctx context.Context, logger logger.ContextLogger) (*coredevice.Device, error) {
	var failures []error
	for _, address := range d.addresses {
		logger.Debug("connecting through remote pairing at ", address)
		connected, err := coredevice.ConnectRemotePairing(ctx, coredevice.RemotePairingOptions{Address: address, Record: d.record, Logger: logger})
		if err == nil || ctx.Err() != nil {
			return connected, err
		}
		if errors.Is(err, pairing.ErrPairVerifyFailed) {
			return nil, &device.Error{Problem: device.ProblemNoTunnel, Err: E.Append(
				E.Cause(err, "the device no longer accepts the pairing for connections over the local network; connect it to this computer with a cable to pair again"),
				forgetRemotePairing(d.udid),
				func(err error) error { return E.Cause(err, "forget the pairing") },
			)}
		}
		failures = append(failures, err)
	}
	return nil, E.Errors(failures...)
}

func remotePairingFile(udid string) (string, error) {
	directory, err := remotePairingDirectory()
	if err != nil {
		return "", err
	}
	return filepath.Join(directory, udid+remotePairingSuffix), nil
}

func forgetRemotePairing(udid string) error {
	file, err := remotePairingFile(udid)
	if err != nil {
		return err
	}
	err = os.Remove(file)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

func remotePairingDirectory() (string, error) {
	configuration, _, err := userDirectories()
	if err != nil {
		return "", err
	}
	return filepath.Join(configuration, remotePairingFolder), nil
}

func remotePairings() (map[string]*pairing.Record, error) {
	directory, err := remotePairingDirectory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	records := make(map[string]*pairing.Record)
	for _, entry := range entries {
		udid, isRecord := strings.CutSuffix(entry.Name(), remotePairingSuffix)
		if !isRecord {
			continue
		}
		if record := readRemotePairing(filepath.Join(directory, entry.Name())); record != nil {
			records[udid] = record
		}
	}
	return records, nil
}

func readRemotePairing(file string) *pairing.Record {
	data, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	record, err := pairing.ParseRecord(data)
	if err != nil {
		return nil
	}
	return record
}

func browseNetwork(ctx context.Context, listed []usbmuxd.Device, udid string) ([]networkDevice, error) {
	records, err := remotePairings()
	if err != nil {
		return nil, err
	}
	for paired := range records {
		if (udid != "" && paired != udid) || slices.ContainsFunc(listed, func(it usbmuxd.Device) bool { return it.UDID == paired }) {
			delete(records, paired)
		}
	}
	if len(records) == 0 {
		return nil, nil
	}
	browseCtx, cancel := context.WithTimeout(ctx, browseTimeout)
	defer cancel()
	var found []networkDevice
	err = mdns.Browse(browseCtx, remotePairingService, func(service mdns.Service) bool {
		for paired, record := range records {
			if !record.MatchesService(service.Text["identifier"], service.Text["authTag"]) {
				continue
			}
			matched := networkDevice{udid: paired, record: record}
			for _, ipv4 := range []bool{true, false} {
				for _, address := range service.Addresses {
					if address.Is4() == ipv4 {
						matched.addresses = append(matched.addresses, netip.AddrPortFrom(address, service.Port))
					}
				}
			}
			found = append(found, matched)
			delete(records, paired)
		}
		return len(records) == 0
	})
	if err == nil {
		err = ctx.Err()
	}
	return found, err
}

func rememberRemotePairing(ctx context.Context, found peer, logger logger.ContextLogger) error {
	file, err := remotePairingFile(found.record.UDID)
	if err != nil {
		return err
	}
	if record := readRemotePairing(file); record != nil {
		accepted, err := remotePairingAccepted(ctx, found, record, logger)
		if err != nil || accepted {
			return err
		}
		logger.Info("the device no longer accepts the pairing for connections over the local network, pairing again")
	}
	conn, err := lockdown.DialService(ctx, found.record, dialer(found.device.ID), lockdown.RemotePairing, logger)
	if err != nil {
		return err
	}
	record, err := pairing.Setup(ctx, conn, pairing.SetupOptions{HostName: Name(), Logger: logger})
	if err != nil {
		return err
	}
	data, err := record.Marshal()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(file, data, 0o600)
}

func remotePairingAccepted(ctx context.Context, found peer, record *pairing.Record, logger logger.ContextLogger) (bool, error) {
	conn, err := lockdown.DialService(ctx, found.record, dialer(found.device.ID), lockdown.RemotePairing, logger)
	if err != nil {
		return false, err
	}
	session, err := pairing.Verify(ctx, conn, record, logger)
	if errors.Is(err, pairing.ErrPairVerifyFailed) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = session.Close()
	return true, nil
}
