package device

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/swipt-automation/swipt/internal/coredevice"
	"github.com/swipt-automation/swipt/internal/coredevice/hid"
)

type PasscodeType int

const (
	PasscodePIN PasscodeType = iota
	PasscodeNumeric
	PasscodeAlphanumeric
)

const maxPasscodeLength = 64

// Read back to back while the display woke, the lock state took 27 ms instead
// of under 1 ms to answer after about 820 reads (iOS and iPadOS 27.2 24B115).
const unlockPoll = 10 * time.Millisecond

const unlockLimit = 4500 * time.Millisecond

func validateAutoUnlock(settings Settings) error {
	passcode := settings.AutoUnlockPasscode
	if passcode == "" {
		return nil
	}
	if len(passcode) > maxPasscodeLength {
		return E.New("the device passcode must contain at most 64 characters")
	}
	switch settings.AutoUnlockPasscodeType {
	case PasscodePIN:
		if len(passcode) != 4 && len(passcode) != 6 {
			return E.New("a fixed-length device PIN must contain 4 or 6 digits")
		}
	case PasscodeNumeric, PasscodeAlphanumeric:
	default:
		return E.New("unknown device passcode type")
	}
	for index := range len(passcode) {
		character := passcode[index]
		if settings.AutoUnlockPasscodeType != PasscodeAlphanumeric && (character < '0' || character > '9') {
			return E.New("a numeric device passcode can contain only digits")
		}
		if _, ok := asciiKeystrokes[character]; !ok {
			return E.New("automatic unlocking supports only printable ASCII characters")
		}
	}
	return nil
}

type deviceUnlockTarget struct {
	connection *Connection
	settings   *Settings
	conn       *conn
	input      *hid.Client
	keys       *keyboard
	logger     logger.ContextLogger
}

func (t *deviceUnlockTarget) unlock(parent context.Context) (consumed bool, err error) {
	ctx, cancel := context.WithTimeout(parent, unlockLimit)
	defer cancel()
	defer func() {
		if err != nil && ctx.Err() != nil && parent.Err() == nil {
			err = E.Cause(err, "the device was not unlocked with an active display within ", unlockLimit)
		}
	}()
	locked, err := t.conn.lockState.locked(ctx, t.conn.device)
	if err != nil {
		return false, err
	}
	if !locked {
		if consumed, locked, err = t.wake(ctx); err != nil || !locked {
			return consumed, err
		}
	}
	if err = t.prepare(ctx); err != nil {
		return true, err
	}
	strokes := common.Map([]byte(t.settings.AutoUnlockPasscode), func(character byte) asciiKeystroke {
		return asciiKeystrokes[character]
	})
	if t.settings.AutoUnlockPasscodeType != PasscodePIN {
		strokes = append(strokes, asciiKeystroke{usage: usageReturn})
	}
	for _, stroke := range strokes {
		if locked, err = t.conn.lockState.locked(ctx, t.conn.device); err != nil || !locked {
			return true, err
		}
		if err = t.key(ctx, stroke); err != nil {
			return true, err
		}
	}
	t.logger.Debug("unlock passcode typed")
	var polled time.Time
	for {
		if err = sleep(ctx, unlockPoll-time.Since(polled)); err != nil {
			return true, err
		}
		polled = time.Now()
		if locked, err = t.conn.lockState.locked(ctx, t.conn.device); err != nil || !locked {
			return true, err
		}
	}
}

// SpringBoard posts com.apple.springboard.lockstate when the device locks or
// unlocks (iOS 27.2 24B115).
const lockStateLife = 30 * time.Second

type lockStateReader struct {
	access sync.Mutex
	conn   *coredevice.LockStateConn

	owner    *conn
	value    bool
	readAt   time.Time
	watching bool
	relayed  func() bool
	unwatch  func()
}

func (r *lockStateReader) recent(ctx context.Context) (bool, error) {
	r.access.Lock()
	if !r.watching && r.owner != nil && r.owner.ctx.Err() == nil {
		changed, relayed, cancel := r.owner.darwin.watch()
		r.watching, r.relayed, r.unwatch = true, relayed, cancel
		go func() {
			for {
				select {
				case <-changed:
					r.forget()
					_, _ = r.recent(r.owner.ctx)
				case <-r.owner.ctx.Done():
					return
				}
			}
		}()
	}
	life := lockStateLife
	if r.relayed == nil || !r.relayed() {
		life = time.Second
	}
	if !r.readAt.IsZero() && time.Since(r.readAt) < life {
		value := r.value
		r.access.Unlock()
		return value, nil
	}
	r.access.Unlock()
	return r.locked(ctx, r.owner.device)
}

func (r *lockStateReader) forget() {
	r.access.Lock()
	r.readAt = time.Time{}
	r.access.Unlock()
}

func (r *lockStateReader) locked(ctx context.Context, device *coredevice.Device) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.access.Lock()
	defer r.access.Unlock()
	for {
		reused := r.conn != nil
		if !reused {
			conn, err := device.DialLockState(ctx)
			if err != nil {
				return false, err
			}
			r.conn = conn
		}
		state, err := r.conn.LockState(ctx)
		if err == nil {
			r.value, r.readAt = state.PasscodeRequired, time.Now()
			return state.PasscodeRequired, nil
		}
		r.conn.Close()
		r.conn = nil
		if !reused || ctx.Err() != nil {
			return false, err
		}
	}
}

func (r *lockStateReader) close() error {
	r.access.Lock()
	defer r.access.Unlock()
	if r.unwatch != nil {
		r.unwatch()
		r.unwatch, r.watching = nil, false
	}
	if r.conn == nil {
		return nil
	}
	err := r.conn.Close()
	r.conn = nil
	return err
}

func (t *deviceUnlockTarget) prepare(ctx context.Context) error {
	started := time.Now()
	if err := t.input.ReleaseAll(ctx); err != nil {
		return err
	}
	if err := t.keys.capsLockKey(ctx, t.input, false); err != nil {
		return err
	}
	transportContext, cancelTransports := context.WithCancel(ctx)
	touchesPrepared, keyboardPrepared := make(chan struct{}), make(chan struct{})
	var touchesErr, keyboardErr error
	go func() {
		defer close(touchesPrepared)
		touchesErr = t.input.PrepareTouches(transportContext)
	}()
	go func() {
		defer close(keyboardPrepared)
		keyboardErr = t.input.PrepareKeyboard(transportContext)
	}()
	defer func() {
		cancelTransports()
		<-touchesPrepared
		<-keyboardPrepared
	}()
	join := func(done <-chan struct{}, err *error) error {
		select {
		case <-done:
			return *err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	_, synchronized := t.conn.localBacklight()
	if !synchronized {
		if err := t.conn.darwin.waitReady(ctx); err != nil {
			return E.Cause(err, "observe SpringBoard following the display")
		}
	}
	wakeSent, dark := false, false
	var darkAt uint64
	var polled time.Time
	for {
		if err := sleep(ctx, unlockPoll-time.Since(polled)); err != nil {
			return err
		}
		polled = time.Now()
		posted, _ := t.conn.darwin.displayChanges()
		state, err := t.conn.backlight(ctx)
		if err != nil {
			return err
		}
		ready := state == "activeOn"
		if !synchronized {
			// CoreDevice kept reporting activeOn up to 0.2 s after SpringBoard blanked
			// the display for a lock, and throughout when Home woke the display before
			// that (iPhone18,2, iOS 27.3 24C5037i).
			if !dark && (state == "off" || state == "inactiveOn") {
				dark, darkAt = true, posted
			}
			current, unblanked := t.conn.darwin.displayChanges()
			ready = ready && unblanked && (!dark || current > darkAt)
		}
		if ready {
			break
		}
		if wakeSent {
			continue
		}
		// Creating the keyboard took 0.38 s, so Home wakes the display
		// meanwhile: it was active 0.48 s after Home, while creating the
		// keyboard first and pressing Home then took 0.38 + 0.28 s (iPad17,1,
		// iPadOS 27.2 24B115, from the iPad's own server).
		var wake func() error
		select {
		case <-keyboardPrepared:
			if keyboardErr != nil {
				return keyboardErr
			}
			wake = func() error { return t.key(ctx, asciiKeystroke{usage: usageBackspace}) }
		default:
			if err = join(touchesPrepared, &touchesErr); err != nil {
				return err
			}
			home := hid.ButtonHome
			home.Hold = 0
			wake = func() error { return t.input.PressButton(ctx, home) }
		}
		if locked, err := t.conn.lockState.locked(ctx, t.conn.device); err != nil || !locked {
			return err
		}
		if err = wake(); err != nil {
			return err
		}
		wakeSent = true
		t.logger.Debug("unlock wake after ", time.Since(started))
	}
	if err := join(keyboardPrepared, &keyboardErr); err != nil {
		return err
	}
	if locked, err := t.conn.lockState.locked(ctx, t.conn.device); err != nil || !locked {
		return err
	}
	if err := t.key(ctx, asciiKeystroke{usage: usageBackspace}); err != nil {
		return err
	}
	t.logger.Debug("unlock backspace after ", time.Since(started))
	return nil
}

func (t *deviceUnlockTarget) wake(ctx context.Context) (consumed, locked bool, err error) {
	state, err := t.conn.backlight(ctx)
	if err != nil || state == "activeOn" || state == "dimmed" {
		return false, false, err
	}
	home := hid.ButtonHome
	home.Hold = 0
	if err = t.input.PressButton(ctx, home); err != nil {
		return true, false, err
	}
	var polled time.Time
	for {
		if err = sleep(ctx, unlockPoll-time.Since(polled)); err != nil {
			return true, false, err
		}
		polled = time.Now()
		if locked, err = t.conn.lockState.locked(ctx, t.conn.device); err != nil || locked {
			return true, locked, err
		}
		active, err := t.activeDisplay(ctx)
		if err != nil || active {
			return true, false, err
		}
	}
}

func (t *deviceUnlockTarget) activeDisplay(ctx context.Context) (bool, error) {
	state, err := t.conn.backlight(ctx)
	return state == "activeOn", err
}

func (t *deviceUnlockTarget) key(ctx context.Context, stroke asciiKeystroke) error {
	input := t.input
	if stroke.shift {
		if err := input.KeyDown(ctx, usageLeftShift); err != nil {
			return err
		}
	}
	if err := input.KeyDown(ctx, stroke.usage); err != nil {
		return err
	}
	if err := input.KeyUp(ctx, stroke.usage); err != nil {
		return err
	}
	if stroke.shift {
		if err := input.KeyUp(ctx, usageLeftShift); err != nil {
			return err
		}
	}
	return nil
}

func (q *hidQueue) checkUnlock(c *conn) bool {
	settings := q.connection.settings.Load()
	if len(q.connection.sessions()) == 0 || settings.AutoUnlockPasscode == "" {
		q.unlockPending = false
		return false
	}
	started := time.Now()
	target := &deviceUnlockTarget{connection: q.connection, settings: settings, conn: c, input: q.client, keys: q.keys, logger: q.logger}
	consumed, err := target.unlock(c.ctx)
	if consumed || err != nil {
		released := c.ctx.Err() == nil
		if released {
			if releaseErr := q.client.ReleaseAll(c.ctx); releaseErr != nil {
				q.logger.Warn(E.Cause(releaseErr, "release input after automatic unlocking"))
				released = false
			}
		}
		if err != nil || !released {
			q.closeClient()
		} else {
			q.resetState()
		}
		q.access.Lock()
		q.queue = slices.DeleteFunc(q.queue, func(item hidItem) bool { return item.event != nil })
		q.access.Unlock()
	}
	if consumed {
		q.logger.Info("automatic unlock finished in ", time.Since(started), ", error: ", err)
	}
	if err != nil && c.ctx.Err() != nil {
		return true
	}
	q.unlockPending = false
	if err != nil {
		q.logger.Warn(E.Cause(err, "automatic unlocking"))
		return true
	}
	return consumed
}
