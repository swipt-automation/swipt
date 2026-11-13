package libSwipt

import (
	"context"
	"encoding/json"
	"runtime"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/graphics"
	"github.com/swipt-automation/swipt/internal/log"
)

const (
	SwipletSourceFolder  = automation.SourceFolder
	SwipletAssetsFolder  = automation.AssetsFolder
	SwipletDataFolder    = automation.DataFolder
	SwipletInterfaceFile = automation.InterfaceFile
	SwipletExtensionFile = automation.ExtensionFile
	SwipletIconFile      = automation.IconFile
)

const (
	CallerApp       = string(automation.CallerApp)
	CallerShortcuts = string(automation.CallerShortcuts)
)

type PlatformOptions struct {
	SwipletDirectory      string
	SwipletCacheDirectory string
	Languages             StringIterator
	OSVersion             string
	AppBundleID           string
	AppVersion            string
	AppBuild              string
}

type PlatformHandler interface {
	OnDialog(dialogID int64, request string)
	OnDialogWithdrawn(dialogID int64)
	OnBackdrop(image []byte)
	OnServiceChanged(swiplet string, enabled bool, status string, message string, since int64)
	OnDatabaseChanged(swiplet string, database string, path string, table string, rowID int64)
	OnSwipletsChanged()
	OnAPIFailed(message string)
	OnShutdownRequested()
	OnRecordingFailed(message string)
	AppActive() bool
}

type Platform struct {
	logFactory *log.Factory
	handler    PlatformHandler
	app        string
	appVersion string
	appBuild   string
	device     *Device
	host       *automation.Host
	dialog     automation.Dialog

	dialogAccess sync.Mutex
	dialogs      map[int64]chan dialogAnswer
	lastDialog   int64

	apiAccess sync.Mutex
	api       *apiState
}

func NewPlatform(options *PlatformOptions, handler PlatformHandler, deviceOptions *DeviceOptions, deviceHandler DeviceHandler) *Platform {
	p := &Platform{logFactory: newLogFactory(), handler: handler, app: options.AppBundleID, appVersion: options.AppVersion, appBuild: options.AppBuild, dialogs: make(map[int64]chan dialogAnswer)}
	p.device = newDevice(deviceOptions, deviceHandler, p.logFactory)
	p.dialog = automation.Dialog{App: p.app, InApp: true, Backdrop: handler.OnBackdrop, Active: handler.AppActive, Ask: func(ctx context.Context, request string) (string, error) {
		return p.ask(ctx, request, handler.OnDialog, handler.OnDialogWithdrawn)
	}}
	languages := iteratorToArray[string](options.Languages)
	osVersion := func() string { return p.device.connection.Phone().Environment().OSVersion }
	if runtime.GOOS == "ios" {
		osVersion = func() string { return options.OSVersion }
	}
	environment := func() device.Environment {
		return device.Environment{OSVersion: osVersion(), Languages: languages}
	}
	p.host = automation.New(automation.Options{
		Device:      p.device.connection.Phone(),
		Environment: environment,
		Directory:   options.SwipletDirectory,
		Caches:      options.SwipletCacheDirectory,
		LogFactory:  p.logFactory,
		Dialog:      p.dialog,
		Graphics: graphics.New(graphics.Options{
			Connection:        p.device.connection,
			Logger:            p.logFactory.NewLogger("graphics"),
			OnRecordingFailed: handler.OnRecordingFailed,
		}),
		OnSwipletsChanged: handler.OnSwipletsChanged,
	})
	p.device.services = p.host
	return p
}

func (p *Platform) Device() *Device {
	return p.device
}

func (p *Platform) Start() error {
	if err := p.device.connection.Start(); err != nil {
		return err
	}
	go func() {
		err := p.host.WatchServices(context.Background(), func(swiplet string, state automation.ServiceState) {
			p.handler.OnServiceChanged(swiplet, state.Enabled, string(state.Status), state.Error, unixMilli(state.Since))
		})
		if err != nil {
			p.logFactory.Logger().Error("watch the services: ", err)
		}
	}()
	go p.host.WatchDatabases(context.Background(), "", func(change automation.DatabaseChange) {
		p.handler.OnDatabaseChanged(change.Swiplet, change.Database, change.Path, change.Table, change.RowID)
	})
	p.host.Start()
	return nil
}

func (p *Platform) Close() error {
	_ = p.SetAPI(nil)
	p.host.Close()
	return p.device.connection.Close()
}

func unixMilli(at time.Time) int64 {
	if at.IsZero() {
		return 0
	}
	return at.UnixMilli()
}

type ActionCall struct {
	cancel context.CancelFunc
	run    func() (string, error)
}

type dialogAnswer struct {
	text      string
	dismissed bool
	failure   string
}

func (a dialogAnswer) result() (string, error) {
	switch {
	case a.failure != "":
		return "", E.New("show the dialog: ", a.failure)
	case a.dismissed:
		return "", automation.ErrDismissed
	}
	return a.text, nil
}

type ActionObserver interface {
	OnDialog(dialogID int64, request string)
	OnDialogWithdrawn(dialogID int64)
	OnValue(value string)
}

func (p *Platform) NewActionCall(swiplet, action, input, caller string, observer ActionObserver, showsInApp bool) *ActionCall {
	ctx, cancel := context.WithCancel(context.Background())
	dialog := automation.Dialog{App: p.app, InApp: showsInApp, Backdrop: p.handler.OnBackdrop, Active: p.handler.AppActive, Ask: func(ctx context.Context, request string) (string, error) {
		return p.ask(ctx, request, observer.OnDialog, observer.OnDialogWithdrawn)
	}}
	return &ActionCall{cancel: cancel, run: func() (string, error) {
		output, err := p.host.Call(ctx, automation.CallRequest{
			Swiplet: swiplet, Action: action, Input: json.RawMessage(input), Caller: automation.Caller(caller), Dialog: dialog,
			Emit: func(value json.RawMessage) { observer.OnValue(string(value)) },
		})
		return string(output), err
	}}
}

func (c *ActionCall) Run() (string, error) {
	defer c.cancel()
	return c.run()
}

func (c *ActionCall) Cancel() {
	c.cancel()
}

type ServiceState struct {
	Enabled bool
	Status  string
	Error   string
	Since   int64
}

func (p *Platform) SetServiceEnabled(swiplet string, enabled bool) error {
	return p.host.SetServiceEnabled(swiplet, enabled)
}

func (p *Platform) LogSwiplet(swiplet string, level int32, message string) {
	p.host.Log(swiplet, log.Level(level), message)
}

func (p *Platform) ServiceState(swiplet string) *ServiceState {
	state, err := p.host.Service(swiplet)
	if err != nil {
		return &ServiceState{Status: string(automation.ServiceStopped)}
	}
	return &ServiceState{Enabled: state.Enabled, Status: string(state.Status), Error: state.Error, Since: unixMilli(state.Since)}
}

func (p *Platform) ask(ctx context.Context, request string, present func(dialogID int64, request string), withdraw func(dialogID int64)) (string, error) {
	answers := make(chan dialogAnswer, 1)
	p.dialogAccess.Lock()
	p.lastDialog++
	id := p.lastDialog
	p.dialogs[id] = answers
	p.dialogAccess.Unlock()
	present(id, request)
	select {
	case answer := <-answers:
		return answer.result()
	case <-ctx.Done():
		if p.takeDialog(id) != nil {
			withdraw(id)
		}
		return "", ctx.Err()
	}
}

func (p *Platform) takeDialog(id int64) chan dialogAnswer {
	p.dialogAccess.Lock()
	defer p.dialogAccess.Unlock()
	answers := p.dialogs[id]
	delete(p.dialogs, id)
	return answers
}

func (p *Platform) AnswerDialog(dialogID int64, answer string) {
	if answers := p.takeDialog(dialogID); answers != nil {
		answers <- dialogAnswer{text: answer}
	}
}

func (p *Platform) DismissDialog(dialogID int64) {
	if answers := p.takeDialog(dialogID); answers != nil {
		answers <- dialogAnswer{dismissed: true}
	}
}

func (p *Platform) FailDialog(dialogID int64, message string) {
	if answers := p.takeDialog(dialogID); answers != nil {
		answers <- dialogAnswer{failure: message}
	}
}
