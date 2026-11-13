//go:build cgo

package computer

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"

	"howett.net/plist"
)

const (
	// vphone guests are built with chip ID 0xFE01, which their UDID starts with
	// (vphone-cli 8c3630c7, Research/Guest/xcode_install_signature_gate.md).
	virtualPrefix     = "0000FE01-"
	launchpadSocket   = "Library/Application Support/vphone-launchpad/control.sock"
	launchpadCLI      = "vphone-launchpad-cli"
	launchTimeout     = 30 * time.Second
	launchRetry       = time.Second
	lockdownRecords   = "/var/root/Library/Lockdown/pair_records"
	localNetworkRules = "/var/preferences/com.apple.networkextension.plist"
	notifications     = "/var/mobile/Library/BulletinBoard/VersionedSectionInfo.plist"
	mobileOwner       = "501:501"
)

var errNotVirtual = E.New("only a virtual machine does this")

type virtualMachine struct {
	name    string
	udid    string
	control vphoned
}

func virtualMachines(ctx context.Context) ([]virtualMachine, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(filepath.Join(home, launchpadSocket)); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	output, err := exec.CommandContext(ctx, launchpadCLI, "vm", "list").Output()
	if errors.Is(err, exec.ErrNotFound) {
		return nil, nil
	}
	if exitError, exited := errors.AsType[*exec.ExitError](err); exited {
		return nil, E.New(launchpadCLI, " vm list: ", strings.TrimSpace(string(exitError.Stderr)))
	}
	if err != nil {
		return nil, err
	}
	var listed []struct {
		Name          string `json:"name"`
		UDID          string `json:"udid"`
		State         string `json:"state"`
		ControlSocket string `json:"controlSocket"`
	}
	if err = json.Unmarshal(output, &listed); err != nil {
		return nil, E.Cause(err, launchpadCLI, " vm list")
	}
	var machines []virtualMachine
	for _, it := range listed {
		if it.State == "running" && it.UDID != "" && it.ControlSocket != "" {
			machines = append(machines, virtualMachine{name: it.Name, udid: it.UDID, control: vphoned(it.ControlSocket)})
		}
	}
	return machines, nil
}

func findVirtualMachine(ctx context.Context, udid string) (*virtualMachine, error) {
	if !strings.HasPrefix(udid, virtualPrefix) {
		return nil, nil
	}
	machines, err := virtualMachines(ctx)
	if err != nil {
		return nil, err
	}
	index := slices.IndexFunc(machines, func(it virtualMachine) bool { return it.udid == udid })
	if index < 0 {
		return nil, E.Extend(ErrDeviceNotListed, udid)
	}
	return &machines[index], nil
}

type vphoned string

func (socket vphoned) call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "unix", string(socket))
	if err != nil {
		return nil, E.Cause(err, "connect to the virtual machine")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	request, err := json.Marshal(map[string]any{"t": "rpc", "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	_, err = conn.Write(append(request, '\n'))
	if err != nil {
		return nil, E.Cause(err, method)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return nil, E.Cause(err, method)
	}
	var reply struct {
		OK     bool           `json:"ok"`
		Error  any            `json:"error"`
		Result map[string]any `json:"result"`
	}
	err = json.Unmarshal(line, &reply)
	if err != nil {
		return nil, E.Cause(err, method)
	}
	if !reply.OK {
		return nil, E.New(method, ": ", fmt.Sprint(reply.Error))
	}
	return reply.Result, nil
}

func (socket vphoned) writeFile(ctx context.Context, file string, data []byte) error {
	_, err := socket.call(ctx, "files.write", map[string]any{"path": file, "content": base64.StdEncoding.EncodeToString(data), "encoding": "base64"})
	return err
}

func (socket vphoned) readFile(ctx context.Context, file string) ([]byte, error) {
	read, err := socket.call(ctx, "files.read", map[string]any{"path": file, "binary": true})
	if err != nil {
		return nil, err
	}
	content, _ := read["content"].(string)
	return base64.StdEncoding.DecodeString(content)
}

func (socket vphoned) editFile(ctx context.Context, file string, edit func([]byte) ([]byte, error)) (bool, error) {
	data, err := socket.readFile(ctx, file)
	if err != nil {
		return false, err
	}
	data, err = edit(data)
	if err != nil {
		return false, E.Cause(err, "edit ", file)
	}
	if data == nil {
		return false, nil
	}
	return true, socket.writeFile(ctx, file, data)
}

func (socket vphoned) chown(ctx context.Context, file string) error {
	_, err := socket.call(ctx, "files.chown", map[string]any{"path": file, "owner": mobileOwner})
	return err
}

func (socket vphoned) restart(ctx context.Context, names ...string) error {
	for _, name := range names {
		listed, err := socket.call(ctx, "processes.list", map[string]any{"filter": name})
		if err != nil {
			return err
		}
		processes, _ := listed["processes"].([]any)
		for _, process := range processes {
			fields, _ := process.(map[string]any)
			if fields["name"] != name {
				continue
			}
			_, err = socket.call(ctx, "processes.kill", map[string]any{"pid": fields["pid"], "signal": "KILL", "force": true})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (socket vphoned) trust(ctx context.Context, pairing *lockdown.Pairing) error {
	err := socket.writeFile(ctx, path.Join(lockdownRecords, pairing.HostID+".plist"), pairing.DeviceRecord)
	if err != nil {
		return err
	}
	return socket.restart(ctx, "lockdownd")
}

func (socket vphoned) applications(ctx context.Context) ([]Application, error) {
	listed, err := socket.call(ctx, "apps.list", nil)
	if err != nil {
		return nil, err
	}
	entries, _ := listed["apps"].([]any)
	applications := make([]Application, 0, len(entries))
	for _, entry := range entries {
		fields, _ := entry.(map[string]any)
		text := func(key string) string {
			value, _ := fields[key].(string)
			return value
		}
		applications = append(applications, Application{BundleIdentifier: text("bundle_id"), Name: text("name"), Version: text("version"), BundleVersion: text("build")})
	}
	return applications, nil
}

func (socket vphoned) launch(ctx context.Context, bundleID string) error {
	ctx, cancel := context.WithTimeout(ctx, launchTimeout)
	defer cancel()
	for {
		_, err := socket.call(ctx, "screen.unlock", nil)
		if err == nil {
			_, err = socket.call(ctx, "apps.launch", map[string]any{"bundle_id": bundleID})
		}
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(launchRetry):
		}
	}
}

func (socket vphoned) container(ctx context.Context, bundleID string) (*Container, error) {
	located, err := socket.call(ctx, "apps.data_dir", map[string]any{"bundle_id": bundleID})
	if err != nil {
		return nil, err
	}
	root, _ := located["data_path"].(string)
	if root == "" {
		return nil, E.New(bundleID, " has no data container")
	}
	return &Container{guestContainer{socket, root}}, nil
}

func (s *Session) Configure(ctx context.Context, bundleID, name string) error {
	if s.virtual == nil {
		return errNotVirtual
	}
	guest := s.virtual.control
	_, err := guest.call(ctx, "settings.set", map[string]any{"domain": "com.apple.springboard", "key": "SBDidShowReorderText", "value": true, "type": "bool"})
	if err != nil {
		return err
	}
	changed, err := guest.editFile(ctx, localNetworkRules, func(data []byte) ([]byte, error) {
		return allowLocalNetwork(data, bundleID)
	})
	if err != nil {
		return err
	}
	if changed {
		err = guest.restart(ctx, "nehelper")
		if err != nil {
			return err
		}
	}
	changed, err = guest.editFile(ctx, notifications, func(data []byte) ([]byte, error) {
		return allowNotifications(data, bundleID, name)
	})
	if err != nil || !changed {
		return err
	}
	if err = guest.chown(ctx, notifications); err != nil {
		return err
	}
	return guest.restart(ctx, "SpringBoard")
}

// A local network decision is a NEPathRule of the NEPathController with
// IgnoreRouteRules, with these values once allowed (NetworkExtension,
// iOS 27.0.1 24A446).
func allowLocalNetwork(data []byte, bundleID string) ([]byte, error) {
	var archive map[string]any
	_, err := plist.Unmarshal(data, &archive)
	if err != nil {
		return nil, err
	}
	objects, _ := archive["$objects"].([]any)
	var rules map[string]any
	for _, object := range objects {
		fields, _ := object.(map[string]any)
		class, isClass := fields["$class"].(plist.UID)
		if !isClass {
			continue
		}
		classFields, _ := objects[class].(map[string]any)
		if classFields["$classname"] == "NEPathController" && fields["IgnoreRouteRules"] == true {
			rules, _ = objects[fields["Rules"].(plist.UID)].(map[string]any)
		}
	}
	listed, _ := rules["NS.objects"].([]any)
	if len(listed) == 0 {
		return nil, E.New("no local network rules")
	}
	var allowed map[string]any
	for _, reference := range listed {
		rule, _ := objects[reference.(plist.UID)].(map[string]any)
		identifier, _ := rule["SigningIdentifier"].(plist.UID)
		if objects[identifier] == bundleID {
			allowed = rule
		}
	}
	if allowed == nil {
		allowed = maps.Clone(objects[listed[0].(plist.UID)].(map[string]any))
		objects = append(objects, bundleID, allowed)
		allowed["SigningIdentifier"] = plist.UID(len(objects) - 2)
		rules["NS.objects"] = append(listed, plist.UID(len(objects)-1))
		archive["$objects"] = objects
	} else if allowed["MulticastPreferenceSet"] == true && allowed["DenyMulticast"] == false {
		return nil, nil
	}
	allowed["MulticastPreferenceSet"] = true
	allowed["DenyMulticast"] = false
	allowed["AllowEmptyDesignatedRequirement"] = true
	allowed["CellularBehavior"] = uint64(0)
	allowed["WiFiBehavior"] = uint64(0)
	return plist.Marshal(archive, plist.BinaryFormat)
}

// SpringBoard keeps the notification settings of an app as a BBSectionInfo archive,
// allowed when authorizationStatus is 2 (BulletinBoard, iOS 27.0.1 24A446).
func allowNotifications(data []byte, bundleID, name string) ([]byte, error) {
	var settings map[string]any
	_, err := plist.Unmarshal(data, &settings)
	if err != nil {
		return nil, err
	}
	sections, _ := settings["sectionInfo"].(map[string]any)
	if _, exists := sections[bundleID]; exists {
		return nil, nil
	}
	for sectionID, section := range sections {
		encoded, _ := section.([]byte)
		var archive map[string]any
		_, err = plist.Unmarshal(encoded, &archive)
		if err != nil {
			continue
		}
		objects, _ := archive["$objects"].([]any)
		var info, sectionSettings map[string]any
		for _, object := range objects {
			fields, _ := object.(map[string]any)
			if _, isInfo := fields["sectionID"]; isInfo {
				info = fields
			}
			if _, isSettings := fields["authorizationStatus"]; isSettings {
				sectionSettings = fields
			}
		}
		if info == nil || sectionSettings == nil || objects[info["sectionID"].(plist.UID)] != sectionID {
			continue
		}
		objects[info["sectionID"].(plist.UID)] = bundleID
		for _, key := range []string{"displayName", "appName"} {
			reference, isReference := info[key].(plist.UID)
			if isReference && reference != 0 {
				objects[reference] = name
			}
		}
		sectionSettings["authorizationStatus"] = uint64(2)
		encoded, err = plist.Marshal(archive, plist.BinaryFormat)
		if err != nil {
			return nil, err
		}
		sections[bundleID] = encoded
		return plist.Marshal(settings, plist.BinaryFormat)
	}
	return nil, E.New("no notification settings to copy")
}
