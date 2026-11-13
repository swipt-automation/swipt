//go:build cgo

package computer

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/api"
	"github.com/swipt-automation/swipt/internal/api/discovery"
	"github.com/swipt-automation/swipt/internal/automation"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/graphics"
	"github.com/swipt-automation/swipt/internal/log"
)

const (
	lockName       = "driver.lock"
	swipletsFolder = "Swiplets"
	shutdownGrace  = 2 * time.Second
)

type Options struct {
	UDID         string
	Passcode     string
	PasscodeType string
	Token        string
	LocalNetwork bool
	DiskImages   []string
	Version      string
	Logs         *log.Factory
	Ready        func(Serving) error
}

func Run(ctx context.Context, options Options) error {
	passcodeKind, err := passcodeType(options.PasscodeType)
	if err != nil {
		return err
	}
	directory, caches, err := userDirectories()
	if err != nil {
		return err
	}
	if err = os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	stored, err := loadSettings(directory)
	if err != nil {
		return err
	}
	url := func(host string) string {
		return "http://" + net.JoinHostPort(host, strconv.Itoa(stored.Port))
	}
	held, err := acquire(filepath.Join(directory, lockName))
	if err != nil {
		return err
	}
	defer held.Close()
	found, err := findPeer(ctx, options.UDID)
	if err != nil {
		return err
	}
	name, err := found.name(ctx)
	if err != nil {
		return err
	}
	var address string
	if !options.LocalNetwork {
		address = "127.0.0.1"
	}
	listener, err := listenConfig.Listen(ctx, "tcp", net.JoinHostPort(address, strconv.Itoa(stored.Port)))
	if E.IsMulti(err, errPortTaken...) {
		stored.Port = randomPort()
		if err = stored.save(directory); err != nil {
			return err
		}
		listener, err = listenConfig.Listen(ctx, "tcp", net.JoinHostPort(address, strconv.Itoa(stored.Port)))
	}
	if err != nil {
		return E.Cause(err, "listen on port ", stored.Port)
	}
	defer listener.Close()
	serving := Serving{Device: name, UDID: options.UDID, API: url("localhost"), MCP: url("localhost") + api.MCPPath}
	if options.LocalNetwork {
		txt := map[string]string{discovery.UDIDKey: options.UDID}
		if options.Token != "" {
			txt[discovery.AuthenticatesKey] = "1"
		}
		host, stopAdvertising, err := advertise(ctx, name, uint16(stored.Port), txt)
		if err != nil {
			return err
		}
		defer stopAdvertising()
		serving.Network = url(host)
	}

	deviceLogger := options.Logs.NewLogger("device")
	diskImages, err := diskImageDirectory(ctx, options.DiskImages, deviceLogger)
	if err != nil {
		return err
	}
	connection := device.New(device.Options{
		Name:               name,
		Connect:            connect(options.UDID, deviceLogger),
		Logger:             deviceLogger,
		Observer:           observer{},
		DiskImageDirectory: diskImages,
	})
	if options.Passcode != "" {
		if err = connection.UpdateSettings(device.Settings{AutoUnlockPasscode: options.Passcode, AutoUnlockPasscodeType: passcodeKind}); err != nil {
			return err
		}
	}
	if err = connection.Start(); err != nil {
		return err
	}
	defer connection.Close()
	swiplets := automation.New(automation.Options{
		Device:      connection.Phone(),
		Environment: connection.Phone().Environment,
		Directory:   filepath.Join(directory, swipletsFolder),
		Caches:      filepath.Join(caches, swipletsFolder),
		LogFactory:  options.Logs,
		Graphics:    graphics.New(graphics.Options{Connection: connection, Logger: options.Logs.NewLogger("graphics")}),
	})
	swiplets.Start()
	defer swiplets.Close()

	stop := make(chan struct{})
	var stopOnce sync.Once
	server := api.New(api.Options{
		Device: connection, Swiplets: swiplets, Logs: options.Logs, Logger: options.Logs.NewLogger("api"),
		AppVersion: options.Version,
		Shutdown:   func() { stopOnce.Do(func() { close(stop) }) },
	})
	var credential *api.Credential
	if options.Token != "" {
		credential = server.NewCredential(options.Token)
	}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener, "", credential) }()
	if err = options.Ready(serving); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-stop:
	case err = <-served:
		if err != nil {
			return E.Cause(err, "serve the API")
		}
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	if err = server.Shutdown(shutdownCtx); err != nil {
		options.Logs.NewLogger("api").Debug("stopped: ", err)
	}
	return nil
}

type Serving struct {
	Device  string `json:"device"`
	UDID    string `json:"udid"`
	API     string `json:"api"`
	MCP     string `json:"mcp"`
	Network string `json:"network,omitempty"`
}

func Mount(ctx context.Context, udid string, diskImages []string, logger log.ContextLogger) error {
	directory, err := diskImageDirectory(ctx, diskImages, logger)
	if err != nil {
		return err
	}
	return device.PrepareDiskImage(ctx, connect(udid, logger), directory, logger)
}

func passcodeType(name string) (device.PasscodeType, error) {
	switch name {
	case "", "pin":
		return device.PasscodePIN, nil
	case "numeric":
		return device.PasscodeNumeric, nil
	case "alphanumeric":
		return device.PasscodeAlphanumeric, nil
	default:
		return 0, E.New("unknown passcode type ", name, "; it is pin, numeric or alphanumeric")
	}
}
