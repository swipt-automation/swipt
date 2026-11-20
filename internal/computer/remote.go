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
	"sync"
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
	remotePairingTimeout = 10 * time.Second
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
		if !isRecord || coredevice.IsGuest(udid) {
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

func browseNetwork(ctx context.Context, listed []usbmuxd.Device, udid string, found func(networkDevice)) error {
	records, err := remotePairings()
	if err != nil {
		return err
	}
	for paired := range records {
		if (udid != "" && paired != udid) || slices.ContainsFunc(listed, func(it usbmuxd.Device) bool { return it.UDID == paired }) {
			delete(records, paired)
		}
	}
	if len(records) == 0 {
		return nil
	}
	browseCtx, cancel := context.WithTimeout(ctx, browseTimeout)
	defer cancel()
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
			delete(records, paired)
			found(matched)
		}
		return len(records) == 0
	})
	if err == nil {
		err = ctx.Err()
	}
	return err
}

type remotePairer struct {
	ctx    context.Context
	cancel context.CancelFunc
	logger logger.ContextLogger
	wake   chan struct{}
	done   chan struct{}

	access sync.Mutex
	peer   peer
	target *coredevice.Device
	paired bool
}

func newRemotePairer(parent context.Context, logger logger.ContextLogger) *remotePairer {
	ctx, cancel := context.WithCancel(parent)
	r := &remotePairer{ctx: ctx, cancel: cancel, logger: logger, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go r.run()
	return r
}

func (r *remotePairer) connect(udid string) func(ctx context.Context) (*coredevice.Device, error) {
	return func(ctx context.Context) (*coredevice.Device, error) {
		found, err := findPeer(ctx, udid)
		if err != nil {
			return nil, err
		}
		connected, err := found.connect(ctx, r.logger)
		if err != nil {
			return nil, err
		}
		r.remember(found, connected)
		return connected, nil
	}
}

func (r *remotePairer) remember(found peer, target *coredevice.Device) {
	if found.network != nil || coredevice.IsGuest(found.record.UDID) {
		return
	}
	r.access.Lock()
	if r.paired {
		r.access.Unlock()
		return
	}
	r.peer, r.target = found, target
	r.access.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *remotePairer) run() {
	defer close(r.done)
	for r.ctx.Err() == nil {
		r.access.Lock()
		found, target, paired := r.peer, r.target, r.paired
		r.access.Unlock()
		if paired {
			return
		}
		if target == nil || target.Closed() {
			select {
			case <-r.ctx.Done():
				return
			case <-r.wake:
			}
			continue
		}
		ctx, cancel := context.WithCancel(r.ctx)
		go func() {
			select {
			case <-target.Done():
				cancel()
			case <-ctx.Done():
			}
		}()
		err := rememberRemotePairing(ctx, found, target, r.logger)
		stopped := ctx.Err() != nil
		cancel()
		if err == nil {
			r.access.Lock()
			r.paired = true
			r.access.Unlock()
			return
		}
		if !stopped {
			r.logger.Warn("pair for connections over the local network: ", err)
			r.access.Lock()
			if r.target == target {
				r.target = nil
			}
			r.access.Unlock()
		}
	}
}

func (r *remotePairer) Close() {
	r.cancel()
	<-r.done
}

func waitUnlocked(ctx context.Context, target *coredevice.Device) error {
	conn, err := target.DialLockState(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	state, err := conn.LockState(ctx)
	if err != nil || !state.PasscodeRequired {
		return err
	}
	if proxy, proxyErr := target.DialShim(ctx, "com.apple.mobile.notification_proxy.shim.remote"); proxyErr == nil {
		stop := context.AfterFunc(ctx, func() {
			_ = proxy.Close()
		})
		observing := true
		for _, name := range []string{"com.apple.mobile.keybagd.lock_status", "com.apple.springboard.lockstate"} {
			if proxy.Send(map[string]any{"Command": "ObserveNotification", "Name": name}) != nil {
				observing = false
				break
			}
		}
		if observing {
			state, err = conn.LockState(ctx)
			if err != nil || !state.PasscodeRequired {
				stop()
				_ = proxy.Close()
				return err
			}
			for {
				message, receiveErr := proxy.Receive()
				if receiveErr != nil {
					stop()
					_ = proxy.Close()
					if ctx.Err() != nil {
						return ctx.Err()
					}
					break
				}
				if message["Command"] == "ProxyDeath" {
					stop()
					_ = proxy.Close()
					break
				}
				if message["Command"] != "RelayNotification" {
					continue
				}
				state, err = conn.LockState(ctx)
				if err != nil || !state.PasscodeRequired {
					stop()
					_ = proxy.Close()
					return err
				}
			}
		} else {
			stop()
			_ = proxy.Close()
		}
	}
	for {
		state, err = conn.LockState(ctx)
		if err != nil || !state.PasscodeRequired {
			return err
		}
		timer := time.NewTimer(time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func rememberRemotePairing(ctx context.Context, found peer, target *coredevice.Device, logger logger.ContextLogger) error {
	file, err := remotePairingFile(found.record.UDID)
	if err != nil {
		return err
	}
	if err = waitUnlocked(ctx, target); err != nil {
		return err
	}
	if record := readRemotePairing(file); record != nil {
		accepted, err := remotePairingAccepted(ctx, found, record, logger)
		if err != nil || accepted {
			return err
		}
		if err = forgetRemotePairing(found.record.UDID); err != nil {
			return err
		}
		logger.Info("the device no longer accepts the pairing for connections over the local network, pairing again")
	}
	setupCtx, cancel := context.WithTimeout(ctx, remotePairingTimeout)
	defer cancel()
	conn, err := lockdown.DialService(setupCtx, found.record, dialer(found.device.ID), lockdown.RemotePairing, logger)
	if err != nil {
		return err
	}
	record, err := pairing.Setup(setupCtx, conn, pairing.SetupOptions{HostName: Name(), Logger: logger})
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
	verifyCtx, cancel := context.WithTimeout(ctx, remotePairingTimeout)
	defer cancel()
	conn, err := lockdown.DialService(verifyCtx, found.record, dialer(found.device.ID), lockdown.RemotePairing, logger)
	if err != nil {
		return false, err
	}
	session, err := pairing.Verify(verifyCtx, conn, record, logger)
	if errors.Is(err, pairing.ErrPairVerifyFailed) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	_ = session.Close()
	return true, nil
}
