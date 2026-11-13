package device

import (
	"context"
	"path"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/apps"
	"github.com/swipt-automation/swipt/internal/coredevice/instruments"
)

const (
	// The tap gave the first 31 bytes of a longer process name (iPadOS 27.3
	// 24C5037i).
	sysmonNameLength = 31
	// The tap reported kernel_task as process 0, and the process list of the
	// app service had every process of the tap but that one (iPadOS 27.3
	// 24C5037i).
	kernelPID            = 0
	usageListInterval    = 5 * time.Second
	usageNewListInterval = time.Second
	usageAppsInterval    = 30 * time.Second
)

type Process struct {
	PID        uint64
	Executable string
	BundleID   string
}

type ProcessUsage struct {
	instruments.ProcessUsage
	Executable string
	BundleID   string
}

type ResourceUsage struct {
	Time      time.Time
	CoreLoads []float64
	System    []any
	Processes []ProcessUsage
}

type usageEntry struct {
	usage ProcessUsage
	since int
}

type usageListing struct {
	processes []apps.Process
	bundles   map[string]string
	err       error
}

func (p *Phone) WatchResourceUsage(
	ctx context.Context, interval time.Duration, processAttributes, systemAttributes []string, emit func(ResourceUsage),
) error {
	for {
		c, operation, done, err := p.begin(ctx)
		if err != nil {
			return err
		}
		err = c.watchUsage(operation, interval, processAttributes, systemAttributes, emit)
		dropped := operation.Err() != nil
		done()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !dropped {
			return err
		}
	}
}

func (p *Phone) SignalProcess(ctx context.Context, pid uint64, signal syscall.Signal) error {
	c, ctx, done, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	running, err := apps.Signal(ctx, c.device, pid, signal)
	if err == nil && !running {
		err = E.Cause1(ErrNotFound, E.New("there is no process ", pid))
	}
	return err
}

func (p *Phone) Processes(ctx context.Context) ([]Process, error) {
	c, ctx, done, err := p.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	listing := c.listUsage(ctx, true)
	if listing.err != nil {
		return nil, listing.err
	}
	return common.Map(listing.processes, func(it apps.Process) Process {
		return Process{PID: it.PID, Executable: it.Executable, BundleID: listing.bundles[appDirectory(it.Executable)]}
	}), nil
}

func appDirectory(executable string) string {
	return strings.TrimPrefix(path.Dir(executable), "/private")
}

func (c *conn) listUsage(ctx context.Context, withApps bool) usageListing {
	var listing usageListing
	listing.processes, listing.err = apps.Processes(ctx, c.device)
	if listing.err != nil || !withApps {
		return listing
	}
	installed, err := apps.List(ctx, c.device)
	if err != nil {
		listing.err = err
		return listing
	}
	listing.bundles = make(map[string]string, len(installed))
	for _, app := range installed {
		if app.Path != "" {
			listing.bundles[strings.TrimPrefix(app.Path, "/private")] = app.BundleID
		}
	}
	return listing
}

func (c *conn) watchUsage(
	ctx context.Context, interval time.Duration, processAttributes, systemAttributes []string, emit func(ResourceUsage),
) error {
	tap, err := instruments.DialSysmon(ctx, ctx, c.device, interval, processAttributes, systemAttributes)
	if err != nil {
		return err
	}
	defer tap.Close()
	namePosition := slices.Index(processAttributes, "name")
	nameOf := func(usage instruments.ProcessUsage) string {
		if namePosition < 0 {
			return ""
		}
		name, _ := usage.Values[namePosition].(string)
		return name
	}
	set := func(entry *usageEntry, position int, value any) {
		if position >= 0 && entry.usage.Values[position] != value {
			entry.usage.Values = slices.Clone(entry.usage.Values)
			entry.usage.Values[position] = value
		}
	}
	table := make(map[uint64]*usageEntry)
	listings := make(chan usageListing, 1)
	var (
		bundles     map[string]string
		executables map[uint64]string
		listing     bool
		started     int
		listedAt    time.Time
		appsAt      time.Time
		unmatched   bool
	)
	locate := func(pid uint64, entry *usageEntry) bool {
		executable, running := executables[pid]
		if !running {
			return false
		}
		if executable != entry.usage.Executable {
			entry.usage.Executable, entry.usage.BundleID = executable, bundles[appDirectory(executable)]
		}
		reported := nameOf(entry.usage.ProcessUsage)
		if name := path.Base(executable); len(reported) == sysmonNameLength && strings.HasPrefix(name, reported) {
			set(entry, namePosition, name)
		}
		return true
	}
	list := func(now time.Time) {
		withApps := bundles == nil || unmatched && now.Sub(appsAt) >= usageAppsInterval
		listing, listedAt = true, now
		started++
		if withApps {
			appsAt = now
		}
		go func() { listings <- c.listUsage(ctx, withApps) }()
	}
	list(time.Now())
	for {
		select {
		case sample := <-tap.Samples():
			now := sample.Time
			reported := make(map[uint64]bool, len(sample.Processes))
			unlisted := false
			for _, usage := range sample.Processes {
				reported[usage.PID] = true
				entry := table[usage.PID]
				created := entry == nil
				if created {
					entry = &usageEntry{since: started}
					table[usage.PID] = entry
				}
				var name string
				if !created {
					name = nameOf(entry.usage.ProcessUsage)
				}
				entry.usage.ProcessUsage = usage
				if name != "" && strings.HasPrefix(name, nameOf(usage)) {
					set(entry, namePosition, name)
				}
				if created {
					unlisted = !locate(usage.PID, entry) || unlisted
				}
			}
			for pid, entry := range table {
				if !reported[pid] {
					set(entry, slices.Index(processAttributes, "cpuUsage"), float64(0))
					set(entry, slices.Index(processAttributes, "powerScore"), float64(0))
				}
			}
			if !listing && (now.Sub(listedAt) >= usageListInterval || unlisted && now.Sub(listedAt) >= usageNewListInterval) {
				list(now)
			}
			usage := ResourceUsage{
				Time: now, CoreLoads: sample.CoreLoads, System: sample.System,
				Processes: make([]ProcessUsage, 0, len(table)),
			}
			for _, entry := range table {
				usage.Processes = append(usage.Processes, entry.usage)
			}
			slices.SortFunc(usage.Processes, func(a, b ProcessUsage) int { return int(a.PID) - int(b.PID) })
			emit(usage)
		case listed := <-listings:
			listing = false
			if listed.err != nil {
				if ctx.Err() == nil {
					c.logger.Debug(E.Cause(listed.err, "list the processes"))
				}
				continue
			}
			if listed.bundles != nil {
				bundles = listed.bundles
				for _, entry := range table {
					entry.usage.Executable = ""
				}
			}
			executables = make(map[uint64]string, len(listed.processes))
			for _, process := range listed.processes {
				executables[process.PID] = process.Executable
			}
			unmatched = false
			for pid, entry := range table {
				switch {
				case locate(pid, entry):
					unmatched = unmatched || entry.usage.BundleID == "" && strings.HasSuffix(path.Dir(entry.usage.Executable), ".app")
				case entry.since < started && pid != kernelPID:
					delete(table, pid)
				}
			}
		case <-tap.Done():
			if err = tap.Err(); err == nil {
				err = E.New("the system monitor tap ended")
			}
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
