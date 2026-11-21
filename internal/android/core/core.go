//go:build android && cgo

package core

import (
	"context"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/android"
	androidpb "github.com/swipt-automation/swipt/internal/android/proto"
	"github.com/swipt-automation/swipt/internal/api"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/graphics"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/proto"
	"github.com/swipt-automation/swipt/internal/tailscale"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

const (
	swipletsFolder    = "Swiplets"
	cachesFolder      = "Caches"
	tailscaleFolder   = "Tailscale"
	apiCloseGrace     = time.Second
	keepaliveInterval = 30 * time.Second
)

type Options struct {
	Directory  string
	APK        string
	Layer      android.Layer
	Root       bool
	AppVersion string
	AppBuild   string
	Logs       *log.Factory
}

type Core struct {
	options    Options
	logger     log.ContextLogger
	identity   android.Identity
	hostname   string
	caches     string
	device     *android.Device
	host       *automation.Host
	local      *grpc.Server
	alerts     alerts
	settings   *settingsService
	tailscale  *tailscale.Controller
	apiAccess  sync.Mutex
	api        *api.Server
	apiClosing bool
}

func Start(options Options) (*Core, error) {
	if options.Root {
		if err := android.ResetScheduling(); err != nil {
			options.Logs.NewLogger("core").Warn(E.Cause(err, "reset the scheduling of the process"))
		}
	}
	device, err := android.New(options.Layer, options.APK, options.Root)
	if err != nil {
		return nil, E.Cause(err, "start the device layer")
	}
	info, err := device.Info()
	if err != nil {
		device.Close()
		return nil, err
	}
	described, err := android.ReadIdentity(context.Background())
	if err != nil {
		device.Close()
		return nil, err
	}
	stored, err := loadSettings(options.Directory)
	if err != nil {
		device.Close()
		return nil, err
	}
	device.SetCredential(effectiveCredential(stored.AutoUnlock, unlockTypeFromProto(unlockTypeProto(stored.UnlockType)), stored.UnlockPasscode))
	label := hostLabel(described.Name, info.Model)
	var hostname string
	if info.MDNSHostname {
		hostname = localHostname(label)
	}
	caches := filepath.Join(options.Directory, cachesFolder)
	c := &Core{
		options: options, logger: options.Logs.NewLogger("core"), identity: described,
		hostname: hostname, caches: caches, device: device,
	}
	c.settings = newSettingsService(c, options.Directory, stored, options.Logs)
	c.host = automation.New(automation.Options{
		Device: device,
		Environment: func() automation.Environment {
			return automation.Environment{OSVersion: strconv.Itoa(device.SDK()), Languages: device.Languages()}
		},
		Directory:  filepath.Join(options.Directory, swipletsFolder),
		Caches:     caches,
		LogFactory: options.Logs,
		Graphics: graphics.New(graphics.Options{
			Screen: device,
			Logger: options.Logs.NewLogger("graphics"),
			OnRecordingFailed: func(message string) {
				c.alerts.send(&androidpb.Alert{Kind: &androidpb.Alert_RecordingFailure{RecordingFailure: &androidpb.RecordingFailure{Message: message}}})
			},
		}),
	})
	c.tailscale = tailscale.NewController(tailscale.Options{
		Directory: filepath.Join(options.Directory, tailscaleFolder),
		Hostname:  label,
		Version:   options.AppVersion,
		Logger:    options.Logs.NewLogger("tailscale"),
		OnStatus:  c.settings.tailscaleStatus,
		OnError:   c.settings.tailscaleFailed,
	})
	c.local = grpc.NewServer(grpc.KeepaliveParams(keepalive.ServerParameters{Time: keepaliveInterval}))
	androidpb.RegisterSystemServiceServer(c.local, &systemService{Core: c})
	androidpb.RegisterSettingsServiceServer(c.local, c.settings)
	androidpb.RegisterSwipletServiceServer(c.local, &swipletService{Core: c})
	androidpb.RegisterInterfaceServiceServer(c.local, &interfaceService{Core: c, sessions: make(map[string]*interfaceSession)})
	c.host.Start()
	c.settings.start()
	return c, nil
}

type servedDevice struct {
	identity android.Identity
}

func (d servedDevice) Name() string {
	return d.identity.Name
}

func (d servedDevice) Identifier() string {
	return d.identity.ID
}

func (servedDevice) Status() *proto.DeviceConnection {
	return &proto.DeviceConnection{State: proto.DeviceConnectionState_DEVICE_CONNECTION_STATE_RUNNING}
}

func (c *Core) publishAPI(publication *api.Publication) error {
	c.apiAccess.Lock()
	if c.apiClosing {
		c.apiAccess.Unlock()
		return nil
	}
	if publication.Empty() {
		server := c.api
		c.api = nil
		c.apiAccess.Unlock()
		if server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), apiCloseGrace)
			_ = server.Shutdown(ctx)
			cancel()
		}
		return nil
	}
	if c.api == nil {
		c.api = api.New(api.Options{
			Device:          servedDevice{c.identity},
			Swiplets:        c.host,
			Logs:            c.options.Logs,
			Logger:          c.options.Logs.NewLogger("api"),
			AppVersion:      c.options.AppVersion,
			AppBuild:        c.options.AppBuild,
			Shutdown:        func(context.Context) { c.settings.unpublish() },
			OnPublishFailed: c.settings.publishFailed,
		})
	}
	server := c.api
	c.apiAccess.Unlock()
	return server.Publish(publication)
}

type connListener struct {
	conn net.Conn
	used bool
}

func (l *connListener) Accept() (net.Conn, error) {
	if l.used {
		return nil, net.ErrClosed
	}
	l.used = true
	return l.conn, nil
}

func (l *connListener) Close() error {
	return nil
}

func (l *connListener) Addr() net.Addr {
	return l.conn.LocalAddr()
}

func (c *Core) Serve(conn net.Conn) {
	_ = c.local.Serve(&connListener{conn: conn})
}

func (c *Core) Close() error {
	c.settings.close()
	c.apiAccess.Lock()
	c.apiClosing = true
	server := c.api
	c.api = nil
	c.apiAccess.Unlock()
	var err error
	if server != nil {
		err = server.Close()
	}
	c.local.Stop()
	c.host.Close()
	c.device.Close()
	return err
}

func hostLabel(name, model string) string {
	label := strings.Join(strings.FieldsFunc(strings.Map(func(char rune) rune {
		switch {
		case char == '\'' || char == '’':
			return -1
		case char < unicode.MaxASCII && (unicode.IsLetter(char) || unicode.IsDigit(char)):
			return char
		}
		return '-'
	}, name), func(char rune) bool { return char == '-' }), "-")
	if label == "" {
		return model
	}
	return label
}
