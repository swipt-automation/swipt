package instruments

import (
	"context"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/dtx"
)

const (
	appStatesService  = "com.apple.instruments.server.services.mobilenotifications"
	appStatesChannel  = 1
	deviceInfoService = "com.apple.instruments.server.services.deviceinfo"
	deviceInfoChannel = 1
)

// appName is the executable's name, and execName the bundle's path as the app
// list reports it (iOS 27.2 24B115).
type AppState struct {
	Name  string
	Path  string
	PID   uint64
	State string
	At    time.Time
}

type AppStates struct {
	conn   *dtx.Conn
	states chan AppState
}

func DialAppStates(ctx, owner context.Context, device *coredevice.Device) (*AppStates, error) {
	conn, err := device.DialService(ctx, serviceName)
	if err != nil {
		return nil, E.Cause(err, "instruments: connect")
	}
	a := &AppStates{states: make(chan AppState, 64)}
	dtxConn, err := dtx.NewConn(owner, conn, func(event dtx.Event) (any, bool) {
		if event.Selector == "applicationStateNotification:" && len(event.Arguments) > 0 {
			if fields, ok := event.Arguments[0].(map[string]any); ok {
				state := AppState{At: time.Now()}
				state.Name, _ = fields["appName"].(string)
				state.Path, _ = fields["execName"].(string)
				state.State, _ = fields["state_description"].(string)
				switch pid := fields["pid"].(type) {
				case uint64:
					state.PID = pid
				case int64:
					state.PID = uint64(pid)
				}
				select {
				case a.states <- state:
				default:
				}
			}
		}
		return nil, true
	})
	if err != nil {
		conn.Close()
		return nil, E.Cause(err, "instruments: initialize DTX")
	}
	a.conn = dtxConn
	err = dtxConn.Notify(ctx, 0, "_notifyOfPublishedCapabilities:", map[string]any{
		"com.apple.private.DTXBlockCompression": int64(0), "com.apple.private.DTXConnection": int64(1),
	})
	if err != nil {
		dtxConn.Close()
		return nil, E.Cause(err, "instruments: announce capabilities")
	}
	if _, err = dtxConn.Invoke(ctx, 0, "_requestChannelWithCode:identifier:", dtx.Int32(appStatesChannel), appStatesService); err != nil {
		dtxConn.Close()
		return nil, E.Cause(err, "instruments: open ", appStatesService)
	}
	if _, err = dtxConn.Invoke(ctx, appStatesChannel, "setApplicationStateNotificationsEnabled:", true); err != nil {
		dtxConn.Close()
		return nil, E.Cause(err, "instruments: enable application state notifications")
	}
	return a, nil
}

func (a *AppStates) States() <-chan AppState {
	return a.states
}

func (a *AppStates) Done() <-chan struct{} {
	return a.conn.Done()
}

func (a *AppStates) Err() error {
	return a.conn.Err()
}

func (a *AppStates) Close() error {
	return a.conn.Close()
}

type DeviceInfo struct {
	conn *dtx.Conn
}

func DialDeviceInfo(ctx, owner context.Context, device *coredevice.Device) (*DeviceInfo, error) {
	conn, err := device.DialService(ctx, serviceName)
	if err != nil {
		return nil, E.Cause(err, "instruments: connect")
	}
	dtxConn, err := dtx.NewConn(owner, conn, func(dtx.Event) (any, bool) {
		return nil, true
	})
	if err != nil {
		conn.Close()
		return nil, E.Cause(err, "instruments: initialize DTX")
	}
	err = dtxConn.Notify(ctx, 0, "_notifyOfPublishedCapabilities:", map[string]any{
		"com.apple.private.DTXBlockCompression": int64(0), "com.apple.private.DTXConnection": int64(1),
	})
	if err != nil {
		dtxConn.Close()
		return nil, E.Cause(err, "instruments: announce capabilities")
	}
	if _, err = dtxConn.Invoke(ctx, 0, "_requestChannelWithCode:identifier:", dtx.Int32(deviceInfoChannel), deviceInfoService); err != nil {
		dtxConn.Close()
		return nil, E.Cause(err, "instruments: open ", deviceInfoService)
	}
	return &DeviceInfo{conn: dtxConn}, nil
}

// runningProcessWithPid: answers the dictionary runningProcesses lists for
// the process, which holds foregroundRunning when its task is running and
// scheduled and its endowment namespaces hold the one tested
// (DVTInstrumentsFoundation 24B5110a 0x262bdb224, 0x262bdab28..0x262bdab80).
func (d *DeviceInfo) Foreground(ctx context.Context, pid uint64) (bool, error) {
	value, err := d.conn.Invoke(ctx, deviceInfoChannel, "runningProcessWithPid:", int64(pid))
	if err != nil {
		return false, E.Cause(err, "instruments: read process ", pid)
	}
	fields, _ := value.(map[string]any)
	running, _ := fields["foregroundRunning"].(bool)
	return running, nil
}

type RunningProcess struct {
	PID        uint64
	BundleID   string
	Foreground bool
}

func (d *DeviceInfo) RunningProcesses(ctx context.Context) ([]RunningProcess, error) {
	value, err := d.conn.Invoke(ctx, deviceInfoChannel, "runningProcesses")
	if err != nil {
		return nil, E.Cause(err, "instruments: list processes")
	}
	list, _ := value.([]any)
	processes := make([]RunningProcess, 0, len(list))
	for _, item := range list {
		fields, _ := item.(map[string]any)
		var process RunningProcess
		switch pid := fields["pid"].(type) {
		case uint64:
			process.PID = pid
		case int64:
			process.PID = uint64(pid)
		}
		process.BundleID, _ = fields["bundleIdentifier"].(string)
		process.Foreground, _ = fields["foregroundRunning"].(bool)
		processes = append(processes, process)
	}
	return processes, nil
}

func (d *DeviceInfo) Err() error {
	return d.conn.Err()
}

func (d *DeviceInfo) Close() error {
	return d.conn.Close()
}
