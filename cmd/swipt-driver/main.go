//go:build cgo

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/computer"
	"github.com/swipt-automation/swipt/internal/coredevice/apps"
	"github.com/swipt-automation/swipt/internal/device"
	"github.com/swipt-automation/swipt/internal/log"
	"github.com/swipt-automation/swipt/internal/usbmuxd"

	"github.com/spf13/cobra"
)

var (
	logs       = log.NewFactory(log.Options{Level: log.LevelInfo, PlatformWriter: writer{}})
	output     = json.NewEncoder(os.Stdout)
	input      = make(chan string, 1)
	udid       string
	diskImages []string
)

var errorCodes = []struct {
	code string
	err  error
}{
	{"device_not_listed", computer.ErrDeviceNotListed},
	{"usbmuxd_unavailable", usbmuxd.ErrUnavailable},
	{"locked", device.ErrLocked},
	{"untrusted", apps.ErrUntrusted},
	{"driver_running", computer.ErrRunning},
}

type failure struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
}

func main() {
	var options computer.Options
	var secretsStdin bool
	command := &cobra.Command{
		Use:   "swipt-driver -d UDID",
		Short: "Serve the Swipt API and MCP from this computer",
		Long: `Serve the Swipt API and MCP from this computer. Run it with "swipt driver".

Each line of standard output is a JSON object: {"result": ...} once the API
answers, or {"error": {"code": ..., "message": ...}} when the driver fails.
Standard error carries the logs. The driver stops when standard input ends.

With --secrets-stdin, the first line of standard input is a JSON object:
"passcode" unlocks the device automatically, and API clients must send "token".`,
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if secretsStdin {
				var secrets struct{ Passcode, Token string }
				if err := readInput(command.Context(), &secrets); err != nil {
					return err
				}
				options.Passcode, options.Token = secrets.Passcode, secrets.Token
			}
			options.UDID = udid
			options.DiskImages = diskImages
			options.Logs = logs
			options.Version = version()
			options.Ready = func(serving computer.Serving) error {
				return output.Encode(map[string]any{"result": serving})
			}
			return computer.Run(command.Context(), options)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.Flags().StringVarP(&udid, "device", "d", "", "the UDID of the device")
	_ = command.MarkFlagRequired("device")
	command.Flags().BoolVar(&secretsStdin, "secrets-stdin", false, "read the passcode and the token from standard input")
	command.Flags().StringVar(&options.PasscodeType, "passcode-type", "", "pin (4 or 6 digits, default), numeric or alphanumeric")
	command.Flags().BoolVar(&options.LocalNetwork, "local-network", false, "serve the local network and advertise there, not only this computer")
	command.Flags().StringArrayVar(&diskImages, "disk-image", nil, "a directory with a Developer Disk Image; the newest one is mounted")
	command.AddCommand(deviceCommand())
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(ctx)
	go watchInput(cancel)
	err := command.ExecuteContext(ctx)
	stopped := ctx.Err() != nil
	cancel()
	stop()
	switch {
	case err == nil:
		return
	case stopped:
		os.Exit(130)
	}
	if encodeErr := output.Encode(map[string]any{"error": describe(err)}); encodeErr != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(1)
}

func describe(err error) failure {
	described := failure{Message: err.Error()}
	for _, it := range errorCodes {
		if errors.Is(err, it.err) {
			described.Code = it.code
			break
		}
	}
	return described
}

func watchInput(stop context.CancelFunc) {
	defer stop()
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	input <- line
	_, _ = io.Copy(io.Discard, reader)
}

func readInput(ctx context.Context, value any) error {
	select {
	case line := <-input:
		return json.Unmarshal([]byte(line), value)
	case <-ctx.Done():
		return E.New("standard input ended before the input")
	}
}

type action func(context.Context, []string) (any, error)

func jsonCommand(use string, args cobra.PositionalArgs, run action) *cobra.Command {
	return &cobra.Command{Use: use, Args: args, RunE: func(command *cobra.Command, args []string) error {
		result, err := run(command.Context(), args)
		if err != nil {
			return err
		}
		return output.Encode(map[string]any{"result": result})
	}}
}

func deviceCommand() *cobra.Command {
	var payloadURL string
	var query bool
	group := &cobra.Command{Use: "device", Hidden: true}
	group.PersistentFlags().StringVarP(&udid, "device", "d", "", "")
	logger := logs.NewLogger("device")
	add := func(use string, args cobra.PositionalArgs, run action) *cobra.Command {
		command := jsonCommand(use, args, run)
		group.AddCommand(command)
		return command
	}
	onDevice := func(use string, args cobra.PositionalArgs, run func(context.Context, *computer.Session, []string) (any, error)) *cobra.Command {
		return add(use, args, func(ctx context.Context, args []string) (any, error) {
			session, err := computer.OpenSession(ctx, udid, logger)
			if err != nil {
				return nil, err
			}
			defer session.Close()
			return run(ctx, session, args)
		})
	}
	inContainer := func(use string, args cobra.PositionalArgs, run func(context.Context, *computer.Container, []string) (any, error)) *cobra.Command {
		return onDevice(use, args, func(ctx context.Context, session *computer.Session, args []string) (any, error) {
			container, err := session.Container(ctx, args[0])
			if err != nil {
				return nil, err
			}
			defer container.Close()
			return run(ctx, container, args[1:])
		})
	}
	add("list", cobra.NoArgs, func(ctx context.Context, _ []string) (any, error) {
		return computer.Devices(ctx)
	})
	add("trust", cobra.NoArgs, func(ctx context.Context, _ []string) (any, error) {
		return nil, computer.Trust(ctx, udid)
	})
	mount := add("mount", cobra.NoArgs, func(ctx context.Context, _ []string) (any, error) {
		return nil, computer.Mount(ctx, udid, diskImages, logger)
	})
	mount.Flags().StringArrayVar(&diskImages, "disk-image", nil, "")
	onDevice("apps", cobra.NoArgs, func(ctx context.Context, session *computer.Session, _ []string) (any, error) {
		return session.Applications(ctx)
	})
	onDevice("install PATH BUNDLE_ID", cobra.ExactArgs(2), func(ctx context.Context, session *computer.Session, args []string) (any, error) {
		return nil, session.Install(ctx, args[0], args[1])
	})
	onDevice("uninstall BUNDLE_ID", cobra.ExactArgs(1), func(ctx context.Context, session *computer.Session, args []string) (any, error) {
		return nil, session.Uninstall(ctx, args[0])
	})
	launch := onDevice("launch BUNDLE_ID [-- ARGUMENTS...]", cobra.MinimumNArgs(1), func(ctx context.Context, session *computer.Session, args []string) (any, error) {
		return nil, session.Launch(ctx, args[0], payloadURL, args[1:])
	})
	launch.Flags().StringVar(&payloadURL, "payload-url", "", "")
	onDevice("terminate BUNDLE_ID", cobra.ExactArgs(1), func(ctx context.Context, session *computer.Session, args []string) (any, error) {
		return nil, session.Terminate(ctx, args[0])
	})
	onDevice("running BUNDLE_ID", cobra.ExactArgs(1), func(ctx context.Context, session *computer.Session, args []string) (any, error) {
		return session.Running(ctx, args[0])
	})
	onDevice("profiles", cobra.NoArgs, func(ctx context.Context, session *computer.Session, _ []string) (any, error) {
		return session.Profiles(ctx)
	})
	onDevice("configure BUNDLE_ID NAME", cobra.ExactArgs(2), func(ctx context.Context, session *computer.Session, args []string) (any, error) {
		return nil, session.Configure(ctx, args[0], args[1])
	})
	onDevice("pair BUNDLE_ID PATH", cobra.ExactArgs(2), func(ctx context.Context, session *computer.Session, args []string) (any, error) {
		return nil, session.Pair(ctx, args[0], args[1])
	})
	inContainer("has BUNDLE_ID PATH", cobra.ExactArgs(2), func(ctx context.Context, container *computer.Container, args []string) (any, error) {
		return container.Has(ctx, args[0])
	})
	sqlite := inContainer("sqlite BUNDLE_ID PATH", cobra.ExactArgs(2), func(ctx context.Context, container *computer.Container, args []string) (any, error) {
		var statements string
		if err := readInput(ctx, &statements); err != nil {
			return nil, err
		}
		return container.SQLite(ctx, args[0], statements, query)
	})
	sqlite.Flags().BoolVar(&query, "query", false, "")
	return group
}

func version() string {
	info, loaded := debug.ReadBuildInfo()
	if !loaded || info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}

type writer struct{}

func (writer) WriteMessage(level log.Level, message string) {
	fmt.Fprintln(os.Stderr, time.Now().Format("15:04:05.000"), levelName(level), message)
}

func levelName(level log.Level) string {
	switch level {
	case log.LevelError:
		return "ERROR"
	case log.LevelWarn:
		return "WARN"
	case log.LevelInfo:
		return "INFO"
	case log.LevelDebug:
		return "DEBUG"
	}
	return "TRACE"
}
