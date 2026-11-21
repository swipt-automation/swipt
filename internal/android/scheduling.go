//go:build android

package android

import (
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"

	"golang.org/x/sys/unix"
)

var rootCgroups = []string{"/dev/cpuset/cgroup.procs", "/dev/cpuctl/cgroup.procs"}

// su of Magisk and KernelSU starts the root process in the cgroups, the SCHED_BATCH, and the uclamp.max of the app
// that asks for it, which Android puts in the background while the grant dialog covers the app.
func ResetScheduling() error {
	pid := []byte(strconv.Itoa(os.Getpid()))
	var errs []error
	for _, procs := range rootCgroups {
		if err := os.WriteFile(procs, pid, 0); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, E.Cause(err, "join ", filepath.Dir(procs)))
		}
	}
	seen := make(map[int]bool)
	for {
		tasks, err := os.ReadDir("/proc/self/task")
		if err != nil {
			return E.Errors(append(errs, err)...)
		}
		var found bool
		for _, task := range tasks {
			tid, _ := strconv.Atoi(task.Name())
			if seen[tid] {
				continue
			}
			seen[tid] = true
			found = true
			priority, err := unix.Getpriority(unix.PRIO_PROCESS, tid)
			if err == nil {
				attr := unix.SchedAttr{Size: unix.SizeofSchedAttr, Policy: unix.SCHED_NORMAL, Nice: int32(20 - priority)}
				err = unix.SchedSetAttr(tid, &attr, 0)
				if err == nil {
					attr.Flags = unix.SCHED_FLAG_UTIL_CLAMP_MIN | unix.SCHED_FLAG_UTIL_CLAMP_MAX
					attr.Util_min, attr.Util_max = math.MaxUint32, math.MaxUint32
					if err = unix.SchedSetAttr(tid, &attr, 0); errors.Is(err, unix.EINVAL) || errors.Is(err, unix.EOPNOTSUPP) {
						err = nil
					}
				}
			}
			if err != nil && !errors.Is(err, unix.ESRCH) {
				errs = append(errs, E.Cause(err, "reset the scheduling of thread ", tid))
			}
		}
		if !found {
			return E.Errors(errs...)
		}
	}
}
