//go:build cgo

package computer

import (
	"context"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/afc"
	"github.com/swipt-automation/swipt/internal/coredevice/apps"
	"github.com/swipt-automation/swipt/internal/coredevice/housearrest"
	"github.com/swipt-automation/swipt/internal/coredevice/instproxy"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"
	"github.com/swipt-automation/swipt/internal/coredevice/misagent"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/usbmuxd"
)

const stagingDirectory = "PublicStaging"

type ListedDevice struct {
	Name   string `json:"name"`
	UDID   string `json:"udid"`
	Paired bool   `json:"paired"`
}

func Devices(ctx context.Context) ([]ListedDevice, error) {
	listed, listErr := usbmuxd.ListDevices(ctx)
	devices := []ListedDevice{}
	for _, found := range preferUSB(listed) {
		name, _ := lockdown.GetValue(ctx, dialer(found.ID), "DeviceName")
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		text, _ := name.(string)
		_, err := usbmuxd.ReadPairRecord(ctx, found.UDID)
		devices = append(devices, ListedDevice{Name: text, UDID: found.UDID, Paired: err == nil})
	}
	network, err := browseNetwork(ctx, listed, "")
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	for _, it := range network {
		devices = append(devices, ListedDevice{Name: it.record.DeviceName, UDID: it.udid, Paired: true})
	}
	if len(devices) == 0 {
		if err = E.Errors(usbmuxdFailure(listErr), err); err != nil {
			return nil, err
		}
	}
	return devices, nil
}

func Trust(ctx context.Context, udid string) error {
	if _, err := usbmuxd.ReadPairRecord(ctx, udid); err == nil {
		return nil
	}
	devices, err := usbmuxd.ListDevices(ctx)
	if err != nil {
		return err
	}
	index := slices.IndexFunc(preferUSB(devices), func(it usbmuxd.Device) bool { return it.UDID == udid })
	if index < 0 {
		return E.Extend(ErrDeviceNotListed, udid)
	}
	found := preferUSB(devices)[index]
	buid, err := usbmuxd.ReadBUID(ctx)
	if err != nil {
		return err
	}
	pairing, err := lockdown.NewPairing(ctx, dialer(found.ID), buid, udid, Name())
	if err != nil {
		return err
	}
	record, err := lockdown.Pair(ctx, dialer(found.ID), pairing)
	if err != nil {
		return err
	}
	return usbmuxd.SavePairRecord(ctx, found.ID, udid, record)
}

type Application struct {
	BundleIdentifier string `json:"bundleIdentifier"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	BundleVersion    string `json:"bundleVersion"`
}

func (s *Session) Applications(ctx context.Context) ([]Application, error) {
	conn, err := s.service(ctx, instproxy.Service)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	listed, err := instproxy.Browse(conn)
	if err != nil {
		return nil, err
	}
	applications := make([]Application, 0, len(listed))
	for _, it := range listed {
		applications = append(applications, Application{BundleIdentifier: it.BundleID, Name: it.Name, Version: it.Version, BundleVersion: it.BundleVersion})
	}
	return applications, nil
}

func (s *Session) Install(ctx context.Context, source, bundleID string) error {
	info, err := os.Stat(source)
	if err != nil {
		return err
	}
	archive := source
	if info.IsDir() {
		packaged, err := os.CreateTemp("", "swipt-*.ipa")
		if err != nil {
			return err
		}
		archive = packaged.Name()
		defer os.Remove(archive)
		err = zipApplication(packaged, source)
		if closeErr := packaged.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return E.Cause(err, "package ", source)
		}
	}
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	conn, err := s.service(ctx, afc.Service)
	if err != nil {
		return err
	}
	files := afc.New(conn)
	defer files.Close()
	err = files.MakeDirectory(stagingDirectory)
	if err != nil {
		return err
	}
	staged := path.Join(stagingDirectory, bundleID+".ipa")
	defer func() {
		if err := files.Remove(staged); err != nil {
			s.logger.Warn("remove ", staged, ": ", err)
		}
	}()
	err = files.Upload(staged, file)
	if err != nil {
		return E.Cause(err, "upload ", source)
	}
	install, err := s.service(ctx, instproxy.Service)
	if err != nil {
		return err
	}
	defer install.Close()
	return instproxy.Install(install, staged, bundleID)
}

func (s *Session) Uninstall(ctx context.Context, bundleID string) error {
	conn, err := s.service(ctx, instproxy.Service)
	if err != nil {
		return err
	}
	defer conn.Close()
	return instproxy.Uninstall(conn, bundleID)
}

func (s *Session) Launch(ctx context.Context, bundleID, payloadURL string, arguments []string) error {
	target, err := s.device(ctx)
	if err != nil {
		return err
	}
	err = apps.Launch(ctx, target, bundleID, payloadURL, arguments, true, true)
	if err == nil {
		return nil
	}
	if locked, lockErr := passcodeRequired(ctx, target); lockErr == nil && locked {
		return E.Cause1(device.ErrLocked, err)
	}
	return err
}

func passcodeRequired(ctx context.Context, target *coredevice.Device) (bool, error) {
	conn, err := target.DialLockState(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	state, err := conn.LockState(ctx)
	return state.PasscodeRequired, err
}

func (s *Session) installed(ctx context.Context, bundleID string) (instproxy.Application, bool, error) {
	conn, err := s.service(ctx, instproxy.Service)
	if err != nil {
		return instproxy.Application{}, false, err
	}
	listed, err := instproxy.Browse(conn)
	conn.Close()
	if err != nil {
		return instproxy.Application{}, false, err
	}
	index := slices.IndexFunc(listed, func(it instproxy.Application) bool { return it.BundleID == bundleID })
	if index < 0 {
		return instproxy.Application{}, false, nil
	}
	return listed[index], true, nil
}

func (s *Session) Terminate(ctx context.Context, bundleID string) error {
	target, processes, err := s.processes(ctx, bundleID)
	if err != nil {
		return err
	}
	for _, process := range processes {
		if _, err = apps.Signal(ctx, target, process.PID, syscall.SIGKILL); err != nil {
			return err
		}
	}
	return nil
}

func (s *Session) Running(ctx context.Context, bundleID string) (bool, error) {
	_, processes, err := s.processes(ctx, bundleID)
	return len(processes) > 0, err
}

func (s *Session) processes(ctx context.Context, bundleID string) (*coredevice.Device, []apps.Process, error) {
	application, found, err := s.installed(ctx, bundleID)
	if err != nil {
		return nil, nil, err
	}
	if !found {
		return nil, nil, E.New(bundleID, " is not installed")
	}
	bundle := strings.TrimPrefix(strings.TrimSuffix(application.Path, "/")+"/", "/private")
	target, err := s.device(ctx)
	if err != nil {
		return nil, nil, err
	}
	processes, err := apps.Processes(ctx, target)
	if err != nil {
		return nil, nil, err
	}
	return target, slices.DeleteFunc(processes, func(process apps.Process) bool {
		return !strings.HasPrefix(strings.TrimPrefix(process.Executable, "/private"), bundle)
	}), nil
}

func (s *Session) container(ctx context.Context, bundleID string) (*afc.Client, error) {
	conn, err := s.service(ctx, housearrest.Service)
	if err != nil {
		return nil, err
	}
	err = housearrest.VendContainer(conn, bundleID)
	if err != nil {
		conn.Close()
		if _, found, lookupErr := s.installed(ctx, bundleID); lookupErr == nil && !found {
			return nil, E.Cause(err, bundleID, " is not an installed user app")
		}
		return nil, err
	}
	return afc.New(conn), nil
}

type Profile struct {
	Name           string    `json:"name"`
	UUID           string    `json:"uuid"`
	TeamIdentifier string    `json:"teamIdentifier"`
	CreationDate   time.Time `json:"creationDate"`
	ExpirationDate time.Time `json:"expirationDate"`
}

func (s *Session) Profiles(ctx context.Context) ([]Profile, error) {
	conn, err := s.service(ctx, misagent.Service)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	listed, err := misagent.Profiles(conn)
	if err != nil {
		return nil, err
	}
	profiles := make([]Profile, 0, len(listed))
	for _, it := range listed {
		profile := Profile{Name: it.Name, UUID: it.UUID, CreationDate: it.CreationDate, ExpirationDate: it.ExpirationDate}
		if len(it.TeamIdentifier) > 0 {
			profile.TeamIdentifier = it.TeamIdentifier[0]
		}
		profiles = append(profiles, profile)
	}
	return profiles, nil
}
