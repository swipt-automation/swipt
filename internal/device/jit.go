package device

import (
	"context"
	"slices"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/apps"
	"github.com/swipt-automation/swipt/internal/coredevice/debugserver"
	"github.com/swipt-automation/swipt/internal/fault"
)

type jitSession struct {
	cancel context.CancelFunc
	done   chan struct{}
}

func (p *Phone) EnableJIT(ctx context.Context, bundleID string, pid uint64) error {
	c, ctx, done, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	if pid == 0 {
		listing := c.listUsage(ctx, true)
		if listing.err != nil {
			return listing.err
		}
		index := slices.IndexFunc(listing.processes, func(it apps.Process) bool {
			return listing.bundles[appDirectory(it.Executable)] == bundleID
		})
		if index < 0 {
			return E.Cause1(fault.ErrNotFound, E.New(strconv.Quote(bundleID), " is not running"))
		}
		pid = listing.processes[index].PID
	}
	p.jitAttach.Lock()
	defer p.jitAttach.Unlock()
	if err := p.cancelJITSession(ctx, pid); err != nil {
		return err
	}
	jit, err := debugserver.AttachJIT(ctx, c.device, pid)
	if err != nil {
		return err
	}
	serveCtx, cancel := context.WithCancel(c.ctx)
	session := &jitSession{cancel: cancel, done: make(chan struct{})}
	p.jitAccess.Lock()
	if p.jitSessions == nil {
		p.jitSessions = make(map[uint64]*jitSession)
	}
	p.jitSessions[pid] = session
	p.jitAccess.Unlock()
	release := p.connection.hold()
	go func() {
		defer release()
		defer close(session.done)
		defer func() {
			cancel()
			p.jitAccess.Lock()
			if p.jitSessions[pid] == session {
				delete(p.jitSessions, pid)
			}
			p.jitAccess.Unlock()
		}()
		if err := jit.Serve(serveCtx); err != nil && serveCtx.Err() == nil {
			p.logger.Error(E.Cause(err, "serve the JIT of ", bundleID))
		}
	}()
	return nil
}

func (p *Phone) cancelJITSession(ctx context.Context, pid uint64) error {
	p.jitAccess.Lock()
	session := p.jitSessions[pid]
	delete(p.jitSessions, pid)
	p.jitAccess.Unlock()
	if session != nil {
		session.cancel()
		select {
		case <-session.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
