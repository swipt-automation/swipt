package device

import (
	"context"
	"errors"
	"math"
	"strconv"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/accessibility"
)

// UIKit dropped touches during a presentation while the accessibility hit test
// already reached the element, and took them again shortly before the
// animation count, which covers every animation of the process, reached zero
// (iPadOS 27.3 24C5037i).
const animationLimit = 1500 * time.Millisecond

type Aim struct {
	Element string
	Point   Point
	Secure  bool
}

func (a Aim) String() string {
	if a.Point == (Point{}) {
		return a.Element
	}
	return a.Element + " at " + pointText(a.Point)
}

func (p *Phone) Tap(ctx context.Context, ref string, timeout time.Duration) error {
	return p.aimed(ctx, ref, timeout, "tap", func(ctx context.Context, s *axSession, _ *PhoneNode, at Point) error {
		return s.tap(ctx, at, 1, 0)
	})
}

func (p *Phone) SetText(ctx context.Context, ref, text string, timeout time.Duration) error {
	return p.aimed(ctx, ref, timeout, "setText", func(ctx context.Context, s *axSession, node *PhoneNode, at Point) error {
		return s.setText(ctx, node, at, text)
	})
}

func (p *Phone) aimed(ctx context.Context, ref string, timeout time.Duration, name string, act func(ctx context.Context, s *axSession, node *PhoneNode, at Point) error) error {
	c, ctx, done, err := p.begin(ctx)
	if err != nil {
		return err
	}
	defer done()
	if err = p.requireScreen(ctx, c); err != nil {
		return err
	}
	geometry, err := c.readGeometry(ctx)
	if err != nil {
		return err
	}
	started := time.Now()
	s := &axSession{phone: p, conn: c, geometry: geometry, settled: started.Add(animationLimit)}
	var deadline time.Time
	if timeout >= 0 {
		deadline = started.Add(timeout)
	}
	var delay time.Duration
	var waited string
	for attempt := 1; ; attempt++ {
		var pending string
		if s.grant, err = p.connection.input.acquire(ctx, c); err != nil {
			return err
		}
		err = c.ax.do(ctx, true, 0, func(ctx context.Context, client *accessibility.RemoteClient) error {
			s.client = client
			node, err := p.element(c, ref)
			if err != nil {
				return err
			}
			at, reason, err := s.aim(ctx, node)
			if err != nil || reason != "" {
				pending = reason
				return err
			}
			s.target.Point = at
			return act(ctx, s, node, at)
		})
		s.grant.release()
		if errors.Is(err, ErrNotAnswering) {
			pending, err = err.Error(), nil
		}
		if err != nil || pending == "" {
			detail := s.target.String() + ", attempt " + strconv.Itoa(attempt)
			if waited != "" {
				detail += ", after " + waited
			}
			p.connection.trace(started, name+".element", detail, time.Since(started))
			break
		}
		waited = pending
		delay = min(max(delay*2, pollFirst), pollLongest)
		if s.animating {
			delay = pollFirst
		}
		if !s.animating && !deadline.IsZero() && time.Now().Add(delay).After(deadline) {
			p.connection.trace(started, name+".refused", pending, time.Since(started))
			if timeout == 0 {
				return E.Extend(ErrTimeout, pending)
			}
			return E.Extend(ErrTimeout, pending, "; gave up after ", timeout)
		}
		if err = sleep(ctx, delay); err != nil {
			return err
		}
		if s.stale {
			if _, err = p.read(ctx, c, s.reread); err != nil {
				return err
			}
		}
	}
	if s.acted {
		c.events.publish(Event{Kind: EventInput})
		c.front.acted()
	}
	return err
}

func (s *axSession) aim(ctx context.Context, target *PhoneNode) (Point, string, error) {
	name := describeElement(target.AX)
	s.target = Aim{Element: name, Point: target.Point, Secure: target.AX.Secure()}
	s.animating, s.stale = false, false
	s.reread = SnapshotOptions{Depth: target.model.read.Depth, Within: target.model.read.Within}
	if !target.HasFrame() {
		return Point{}, "", E.New(name, " has no position on the screen; perform one of its actions instead")
	}
	s.stale = true
	switch {
	case !target.AX.Enabled:
		return Point{}, name + " is disabled", nil
	case !target.Shown():
		return Point{}, name + " is off the screen", nil
	case target.Covered:
		return Point{}, name + " is covered", nil
	}
	s.stale = false
	at := target.Point
	batch := &accessibility.Batch{}
	mapped := batch.Mapped(target.AX)
	attributes := batch.Attributes(target.AX)
	hit := batch.HitTest(s.geometry.digitizer(at), s.geometry.display.ID)
	animating := batch.AnimationsInProgress(target.AX)
	if err := s.client.Run(ctx, batch); err != nil {
		return Point{}, "", err
	}
	previous := s.previous
	s.previous = attributes.Value.Frame
	switch {
	case mapped.Code == 0 && !mapped.Value:
		return Point{}, name + " is gone", nil
	case mapped.Code != 0 || attributes.Code != 0:
		return Point{}, name + " did not answer", nil
	case !attributes.Value.Enabled:
		return Point{}, name + " is disabled", nil
	case previous != nil && !nearFrame(attributes.Value.Frame, previous):
		return Point{}, name + " is moving", nil
	}
	if fresh := attributes.Value; fresh.Label != "" || fresh.Identifier != "" || fresh.Value != "" {
		name = describeElement(fresh)
	}
	s.target.Element, s.target.Secure = name, attributes.Value.Secure()
	if animating.Code == 0 && animating.Value && time.Now().Before(s.settled) {
		s.animating = true
		return Point{}, name + " is animating", nil
	}
	if fresh := attributes.Value.Frame; fresh != nil {
		rect := target.space.rect(*fresh)
		shown := rect.Intersect(Rect{Width: s.geometry.Width, Height: s.geometry.Height})
		if !rect.Empty() && shown.Empty() {
			s.stale = true
			return Point{}, name + " is off the screen, at " + pointText(Point{rect.X, rect.Y}) + " sized " + pointText(Point{rect.Width, rect.Height}), nil
		}
		if !rect.Empty() && !rect.Contains(at) {
			at = shown.Center()
			again := &accessibility.Batch{}
			hit = again.HitTest(s.geometry.digitizer(at), s.geometry.display.ID)
			if err := s.client.Run(ctx, again); err != nil {
				return Point{}, "", err
			}
		}
	}
	if hit.Code != 0 {
		return Point{}, "the device does not tell which element is at " + pointText(at), nil
	}
	var reached accessibility.Element
	node := target.model.Node(hit.Value.Ref)
	switch {
	case hit.Value.Ref == target.AX.Ref, node != nil && (target.Contains(node) || node.AX.AccessibilityElement && node.Contains(target)):
		return at, "", nil
	case node != nil:
		reached = node.AX
	default:
		inside, known, err := s.inside(ctx, target, hit.Value)
		if err != nil {
			return Point{}, "", err
		}
		if inside {
			return at, "", nil
		}
		read := &accessibility.Batch{}
		element := read.Attributes(hit.Value)
		if err = s.client.Run(ctx, read); err != nil {
			return Point{}, "", err
		}
		if !known && target.model.read.Within == "" && element.Code == 0 && sameWindow(target, element.Value) {
			return at, "", nil
		}
		reached = element.Value
	}
	if target.Inert {
		return Point{}, name + " is behind a modal view, as a sheet or an alert", nil
	}
	reason := "a tap at " + pointText(at) + " would reach " + describeElement(reached) + " of " + s.process(ctx, reached.Ref.PID) + ", not " + name +
		" of " + s.process(ctx, target.AX.Ref.PID)
	if reached.Ref.PID == target.AX.Ref.PID {
		reason += "; input.tap at the element's point taps there regardless"
	}
	return Point{}, reason, nil
}

func (s *axSession) process(ctx context.Context, pid uint64) string {
	text := "process " + strconv.FormatUint(pid, 10)
	bundleID := s.conn.front.bundleOf(pid)
	if springboard, err := s.conn.springboardPID(ctx); bundleID == "" && err == nil && pid == springboard {
		bundleID = springboardBundleID
	}
	if bundleID == "" {
		return text
	}
	return bundleID + " (" + text + ")"
}

func (s *axSession) inside(ctx context.Context, target *PhoneNode, hit accessibility.Element) (inside, known bool, err error) {
	current := hit
	for range accessibility.MaxDepth + 1 {
		if node := target.model.Node(current.Ref); node != nil {
			return target.Contains(node) || node.AX.AccessibilityElement && node.Contains(target), true, nil
		}
		batch := &accessibility.Batch{}
		parent := batch.Parent(current)
		if err = s.client.Run(ctx, batch); err != nil {
			return false, false, err
		}
		if parent.Code != 0 || parent.Value.Application() {
			return false, false, nil
		}
		current = parent.Value
	}
	return false, false, nil
}

func sameWindow(target *PhoneNode, hit accessibility.Element) bool {
	context := hit.Context
	if node := target.model.Node(hit.Ref); node != nil {
		context = node.context()
	}
	if own := target.context(); context != 0 && own != 0 {
		return context == own
	}
	return hit.Ref.PID == target.AX.Ref.PID
}

func nearFrame(first, second *accessibility.LayoutFrame) bool {
	if first == nil || second == nil {
		return first == second
	}
	return math.Abs(first.X-second.X) < 1 && math.Abs(first.Y-second.Y) < 1 && math.Abs(first.Width-second.Width) < 1 && math.Abs(first.Height-second.Height) < 1
}
