//go:build cgo

package computer

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"maps"
	"net"
	"path"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/lockdown"
	"github.com/swipt-automation/swipt/internal/usbmuxd"

	"howett.net/plist"
)

const (
	lockdownRecords      = "/var/root/Library/Lockdown/pair_records"
	localNetworkRules    = "/var/preferences/com.apple.networkextension.plist"
	notificationSettings = "/var/mobile/Library/BulletinBoard/VersionedSectionInfo.plist"
	mobileOwner          = "501:501"
)

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

func GuestPair(ctx context.Context, udid, socket string) error {
	_, err := usbmuxd.ReadPairRecord(ctx, udid)
	if err == nil {
		return nil
	}
	devices, err := usbmuxd.ListDevices(ctx)
	if err != nil {
		return err
	}
	found := common.Find(preferUSB(devices), func(it usbmuxd.Device) bool { return it.UDID == udid })
	if found.ID == 0 {
		return E.Extend(ErrDeviceNotListed, udid)
	}
	buid, err := usbmuxd.ReadBUID(ctx)
	if err != nil {
		return err
	}
	pairing, err := lockdown.NewPairing(ctx, dialer(found.ID), buid, udid, Name())
	if err != nil {
		return err
	}
	guest := vphoned(socket)
	err = guest.writeFile(ctx, path.Join(lockdownRecords, pairing.HostID+".plist"), pairing.DeviceRecord)
	if err != nil {
		return err
	}
	err = usbmuxd.SavePairRecord(ctx, found.ID, udid, pairing.HostRecord)
	if err != nil {
		return err
	}
	return guest.restart(ctx, "lockdownd")
}

func (socket vphoned) chown(ctx context.Context, file string) error {
	_, err := socket.call(ctx, "files.chown", map[string]any{"path": file, "owner": mobileOwner})
	return err
}

func GuestConfigure(ctx context.Context, socket, bundleID, name string) error {
	guest := vphoned(socket)
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
	changed, err = guest.editFile(ctx, notificationSettings, func(data []byte) ([]byte, error) {
		return allowNotifications(data, bundleID, name)
	})
	if err != nil || !changed {
		return err
	}
	if err = guest.chown(ctx, notificationSettings); err != nil {
		return err
	}
	return guest.restart(ctx, "SpringBoard")
}

// A local network decision is a NEPathRule of the NEPathController with
// IgnoreRouteRules, with these values once allowed (NetworkExtension, iPhone18,2,
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
// allowed when authorizationStatus is 2 (BulletinBoard, iPhone18,2, iOS 27.0.1 24A446).
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
