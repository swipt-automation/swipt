//go:build cgo

package main

import (
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

	"github.com/swipt-automation/swipt/internal/computer"
	"github.com/swipt-automation/swipt/internal/log"

	"github.com/spf13/cobra"
)

const exitDeviceNotListed = 3

var (
	options      computer.Options
	pairOptions  computer.PairOptions
	pairOutput   string
	mountOptions computer.MountOptions
)

func main() {
	command := &cobra.Command{
		Use:           "swipt-driver [-d NAME|UDID]",
		Short:         "Serve the Swipt API and MCP from this computer",
		Args:          cobra.NoArgs,
		RunE:          run,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	command.Flags().StringVarP(&options.Device, "device", "d", "", "the device, by name or UDID")
	command.Flags().StringVar(&options.Passcode, "passcode", "", "unlock device with this passcode")
	command.Flags().StringVar(&options.PasscodeType, "passcode-type", "", "pin (4 or 6 digits, default), numeric or alphanumeric")
	command.Flags().StringVar(&options.DiskImage, "disk-image", "", "the directory of the Developer Disk Image to mount")
	pairCommand := &cobra.Command{
		Use:    "pair",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   runPair,
	}
	pairCommand.Flags().StringVarP(&pairOptions.Device, "device", "d", "", "")
	pairCommand.Flags().StringVarP(&pairOutput, "output", "o", "", "")
	mountCommand := &cobra.Command{
		Use:    "mount",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE:   runMount,
	}
	mountCommand.Flags().StringVarP(&mountOptions.Device, "device", "d", "", "")
	mountCommand.Flags().StringVar(&mountOptions.DiskImage, "disk-image", "", "")
	unzipCommand := &cobra.Command{
		Use:    "unzip ARCHIVE DIRECTORY",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return computer.Unzip(args[0], args[1])
		},
	}
	command.AddCommand(pairCommand, mountCommand, guestCommand(), deviceCommand(), unzipCommand)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err := command.ExecuteContext(ctx)
	interrupted := ctx.Err() != nil
	stop()
	switch {
	case err == nil:
		return
	case interrupted:
		os.Exit(130)
	}
	command.PrintErrln(command.ErrPrefix(), err.Error())
	if errors.Is(err, computer.ErrDeviceNotListed) {
		os.Exit(exitDeviceNotListed)
	}
	os.Exit(1)
}

func guestCommand() *cobra.Command {
	var udid, socket string
	group := &cobra.Command{Use: "guest", Hidden: true}
	group.PersistentFlags().StringVarP(&udid, "device", "d", "", "")
	group.PersistentFlags().StringVar(&socket, "socket", "", "")
	add := func(use string, args cobra.PositionalArgs, run func(context.Context, []string) error) {
		group.AddCommand(&cobra.Command{Use: use, Args: args, RunE: func(command *cobra.Command, args []string) error {
			return run(command.Context(), args)
		}})
	}
	add("pair", cobra.NoArgs, func(ctx context.Context, _ []string) error {
		return computer.GuestPair(ctx, udid, socket)
	})
	add("configure BUNDLE_ID NAME", cobra.ExactArgs(2), func(ctx context.Context, args []string) error {
		return computer.GuestConfigure(ctx, socket, args[0], args[1])
	})
	group.AddCommand(containerCommands(func(ctx context.Context, bundleID string) (*computer.Container, error) {
		return computer.OpenGuestContainer(ctx, socket, bundleID)
	})...)
	return group
}

func deviceCommand() *cobra.Command {
	var udid, payloadURL string
	group := &cobra.Command{Use: "device", Hidden: true}
	group.PersistentFlags().StringVarP(&udid, "device", "d", "", "")
	logger := log.NewFactory(log.Options{Level: log.LevelInfo, PlatformWriter: writer{}}).NewLogger("device")
	add := func(use string, args cobra.PositionalArgs, run func(context.Context, []string) (any, error)) *cobra.Command {
		command := &cobra.Command{Use: use, Args: args, RunE: func(command *cobra.Command, args []string) error {
			result, err := run(command.Context(), args)
			if err != nil || result == nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(result)
		}}
		group.AddCommand(command)
		return command
	}
	add("list", cobra.NoArgs, func(ctx context.Context, _ []string) (any, error) {
		return computer.Devices(ctx)
	})
	add("trust", cobra.NoArgs, func(ctx context.Context, _ []string) (any, error) {
		return nil, computer.Trust(ctx, udid)
	})
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
	group.AddCommand(containerCommands(func(ctx context.Context, bundleID string) (*computer.Container, error) {
		return computer.OpenContainer(ctx, udid, bundleID, logger)
	})...)
	onDevice("profiles", cobra.NoArgs, func(ctx context.Context, session *computer.Session, _ []string) (any, error) {
		return session.Profiles(ctx)
	})
	return group
}

func containerCommands(open func(context.Context, string) (*computer.Container, error)) []*cobra.Command {
	var query bool
	add := func(use string, args cobra.PositionalArgs, run func(context.Context, *computer.Container, []string) (any, error)) *cobra.Command {
		return &cobra.Command{Use: use, Args: args, RunE: func(command *cobra.Command, args []string) error {
			container, err := open(command.Context(), args[0])
			if err != nil {
				return err
			}
			defer container.Close()
			result, err := run(command.Context(), container, args[1:])
			if err != nil || result == nil {
				return err
			}
			return json.NewEncoder(command.OutOrStdout()).Encode(result)
		}}
	}
	has := add("has BUNDLE_ID PATH", cobra.ExactArgs(2), func(ctx context.Context, container *computer.Container, args []string) (any, error) {
		return container.Has(ctx, args[0])
	})
	push := add("push BUNDLE_ID SOURCE PATH", cobra.ExactArgs(3), func(ctx context.Context, container *computer.Container, args []string) (any, error) {
		return nil, container.Push(ctx, args[0], args[1])
	})
	sqlite := add("sqlite BUNDLE_ID PATH", cobra.ExactArgs(2), func(ctx context.Context, container *computer.Container, args []string) (any, error) {
		statements, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, err
		}
		rows, err := container.SQLite(ctx, args[0], string(statements), query)
		if err != nil || !query {
			return nil, err
		}
		return rows, nil
	})
	sqlite.Flags().BoolVar(&query, "query", false, "")
	return []*cobra.Command{has, push, sqlite}
}

func run(command *cobra.Command, _ []string) error {
	options.Logs = log.NewFactory(log.Options{Level: log.LevelInfo, PlatformWriter: writer{}})
	options.Version = version()
	options.Output = command.OutOrStdout()
	return computer.Run(command.Context(), options)
}

func runPair(command *cobra.Command, _ []string) error {
	pairOptions.Logs = log.NewFactory(log.Options{Level: log.LevelInfo, PlatformWriter: writer{}})
	record, err := computer.Pair(command.Context(), pairOptions)
	if err != nil {
		return err
	}
	if pairOutput == "" {
		_, err = command.OutOrStdout().Write(record)
		return err
	}
	return os.WriteFile(pairOutput, record, 0o600)
}

func runMount(command *cobra.Command, _ []string) error {
	mountOptions.Logs = log.NewFactory(log.Options{Level: log.LevelInfo, PlatformWriter: writer{}})
	return computer.Mount(command.Context(), mountOptions)
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
