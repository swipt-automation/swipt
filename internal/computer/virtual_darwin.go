//go:build cgo

package computer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// vphone-vm runs the machine whose config.plist is named by --config, and keeps
// udid-prediction.txt and the vphoned control socket vphone.sock beside it
// (vphone-cli ba9d0230, VPhoneBootCommand.swift, VPhoneVirtualMachine.swift,
// VPhoneVirtualMachineAppDelegate.swift).
func virtualMachines() ([]virtualMachine, error) {
	var (
		processes []unix.KinfoProc
		err       error
	)
	for range 4 {
		processes, err = unix.SysctlKinfoProcSlice("kern.proc.uid", os.Getuid())
		if !errors.Is(err, unix.ENOMEM) {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	var machines []virtualMachine
	for _, process := range processes {
		if unix.ByteSliceToString(process.Proc.P_comm[:]) != "vphone-vm" {
			continue
		}
		data, err := unix.SysctlRaw("kern.procargs2", int(process.Proc.P_pid))
		if err != nil {
			continue
		}
		config := machineConfig(processArguments(data))
		if config == "" {
			continue
		}
		directory := filepath.Dir(config)
		prediction, err := os.ReadFile(filepath.Join(directory, "udid-prediction.txt"))
		if err != nil {
			continue
		}
		for line := range strings.Lines(string(prediction)) {
			if udid, found := strings.CutPrefix(strings.TrimSpace(line), "UDID="); found {
				machines = append(machines, virtualMachine{udid: udid, control: vphoned(filepath.Join(directory, "vphone.sock"))})
				break
			}
		}
	}
	return machines, nil
}

// KERN_PROCARGS2 is argc as a 32-bit integer, the executable path, NUL padding,
// then argc NUL-terminated arguments and the environment
// (xnu bsd/kern/kern_sysctl.c, sysctl_procargsx).
func processArguments(data []byte) []string {
	if len(data) < 4 {
		return nil
	}
	count := int(int32(binary.LittleEndian.Uint32(data)))
	if count <= 0 {
		return nil
	}
	_, data, found := bytes.Cut(data[4:], []byte{0})
	if !found {
		return nil
	}
	fields := strings.SplitN(strings.TrimLeft(string(data), "\x00"), "\x00", count+1)
	return fields[:min(count, len(fields))]
}

func machineConfig(arguments []string) string {
	for index := 1; index < len(arguments); index++ {
		for _, name := range []string{"--config", "-c"} {
			if arguments[index] == name && index+1 < len(arguments) {
				return arguments[index+1]
			}
			if value, found := strings.CutPrefix(arguments[index], name+"="); found {
				return value
			}
		}
	}
	return ""
}
