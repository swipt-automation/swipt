package device

import (
	"context"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/hid"
)

// dtuhidd drops a position-only digitizer event that arrives within 16 ms of
// the last event it produced (DDI 27B48 0x100040458); 20 ms leaves room for
// delivery jitter.
const (
	touchFrame    = 20 * time.Millisecond
	swipeDuration = 250 * time.Millisecond
	dragDuration  = 600 * time.Millisecond
	easeSteps     = 12
	pinchDuration = 500 * time.Millisecond
	pinchRadius   = 60.0
)

type TouchSample struct {
	X, Y float64
	T    time.Duration
}

func tap(ctx context.Context, geometry phoneGeometry, input *hid.Client, at Point, count int, hold time.Duration) error {
	if hold <= 0 {
		hold = tapHold
	}
	touch := []hid.Touch{geometry.touch(at)}
	for index := range max(count, 1) {
		if index > 0 {
			if err := sleep(ctx, doubleTapGap); err != nil {
				return err
			}
		}
		if err := input.SetTouches(ctx, touch, hid.EdgeNone); err != nil {
			return err
		}
		if err := sleep(ctx, hold); err != nil {
			return err
		}
		if err := input.LiftTouches(ctx, touch); err != nil {
			return err
		}
	}
	return nil
}

// A move that reached its end in one frame, with no position in between, did
// not reliably scroll (iOS 27.2 24B115).
const moveMinimum = 2 * touchFrame

func SwipePath(from, to Point, duration time.Duration) ([][]TouchSample, error) {
	if duration <= 0 {
		duration = swipeDuration
	}
	if duration < moveMinimum {
		return nil, E.New("a swipe needs at least ", moveMinimum.Milliseconds(), " ms")
	}
	return [][]TouchSample{{{from.X, from.Y, 0}, {to.X, to.Y, duration}}}, nil
}

// A scroll view kept the speed of the last movement after the finger lifted,
// even when it rested first (iOS 27.2 24B5110a).
func DragPath(from, to Point, duration, press time.Duration) ([][]TouchSample, error) {
	if duration <= 0 {
		duration = dragDuration
	}
	if duration < moveMinimum {
		return nil, E.New("a drag needs at least ", moveMinimum.Milliseconds(), " ms")
	}
	path := make([]TouchSample, 0, easeSteps+3)
	path = append(path, TouchSample{from.X, from.Y, 0})
	for step := range easeSteps + 1 {
		progress := float64(step) / easeSteps
		remaining := 1 - progress
		eased := 1 - remaining*remaining*remaining
		path = append(path, TouchSample{from.X + (to.X-from.X)*eased, from.Y + (to.Y-from.Y)*eased, press + time.Duration(progress*float64(duration))})
	}
	return [][]TouchSample{append(path, TouchSample{to.X, to.Y, press + duration})}, nil
}

func pinchPath(geometry phoneGeometry, center Point, scale float64, duration time.Duration) ([][]TouchSample, error) {
	if err := geometry.check(center); err != nil {
		return nil, err
	}
	if scale <= 0 {
		return nil, E.New("the pinch scale must be positive")
	}
	if duration <= 0 {
		duration = pinchDuration
	}
	limit := min(center.X, geometry.Width-center.X, center.Y, geometry.Height-center.Y) - phoneEdgeRegion - 1
	if limit < 10 {
		return nil, E.New("the pinch center is too close to the screen edge")
	}
	start, end := pinchRadius, pinchRadius*scale
	if scale < 1 {
		start, end = pinchRadius/scale, pinchRadius
	}
	if largest := max(start, end); largest > limit {
		start, end = start*limit/largest, end*limit/largest
	}
	finger := func(sign float64) []TouchSample {
		return []TouchSample{
			{center.X + sign*start, center.Y, 0},
			{center.X + sign*end, center.Y, duration},
			{center.X + sign*end, center.Y, duration},
		}
	}
	return [][]TouchSample{finger(-1), finger(1)}, nil
}

func checkFingers(geometry phoneGeometry, fingers [][]TouchSample) error {
	if len(fingers) == 0 || len(fingers) > 2 {
		return E.New("a touch needs one or two fingers")
	}
	for _, finger := range fingers {
		if len(finger) == 0 {
			return E.New("every finger needs at least one point")
		}
		if finger[0].T != 0 || finger[len(finger)-1].T != fingers[0][len(fingers[0])-1].T {
			return E.New("the fingers must touch at 0 ms and lift at the same time")
		}
		for index, sample := range finger {
			if err := geometry.check(Point{sample.X, sample.Y}); err != nil {
				return err
			}
			if index > 0 && sample.T < finger[index-1].T {
				return E.New("touch times must not decrease")
			}
		}
	}
	return nil
}

func play(ctx context.Context, geometry phoneGeometry, input *hid.Client, fingers [][]TouchSample) error {
	end := fingers[0][len(fingers[0])-1].T
	edge := hid.EdgeNone
	if len(fingers) == 1 {
		edge = geometry.edge(Point{fingers[0][0].X, fingers[0][0].Y})
	}
	started := time.Now()
	for frame := 0; ; frame++ {
		elapsed := min(time.Duration(frame)*touchFrame, end)
		touches := common.Map(fingers, func(finger []TouchSample) hid.Touch {
			return geometry.touch(sampleAt(finger, elapsed))
		})
		if err := input.SetTouches(ctx, touches, edge); err != nil {
			return err
		}
		if elapsed == end {
			return input.LiftTouches(ctx, touches)
		}
		if err := sleep(ctx, time.Until(started.Add(time.Duration(frame+1)*touchFrame))); err != nil {
			return err
		}
	}
}

func sampleAt(finger []TouchSample, at time.Duration) Point {
	if at <= finger[0].T {
		return Point{finger[0].X, finger[0].Y}
	}
	for index := 1; index < len(finger); index++ {
		previous, next := finger[index-1], finger[index]
		if at <= next.T {
			fraction := 1.0
			if next.T > previous.T {
				fraction = float64(at-previous.T) / float64(next.T-previous.T)
			}
			return Point{previous.X + (next.X-previous.X)*fraction, previous.Y + (next.Y-previous.Y)*fraction}
		}
	}
	last := finger[len(finger)-1]
	return Point{last.X, last.Y}
}

var phoneKeyUsages = map[string]uint8{
	"return": usageReturn, "backspace": usageBackspace, "forwardDelete": usageDeleteForward,
	"tab": usageTab, "escape": usageEscape, "space": usageSpace,
	"left": usageLeft, "right": usageRight, "up": usageUp, "down": usageDown,
	"home": usageHome, "end": usageEnd, "pageUp": usagePageUp, "pageDown": usagePageDown,
}

var phoneModifierUsages = map[string]uint8{
	"cmd": usageLeftGUI, "shift": usageLeftShift, "ctrl": usageLeftControl, "alt": usageLeftAlt,
}

func parseKeyCombo(combo string) ([]uint8, error) {
	var modifiers []string
	key := combo
	if split := strings.LastIndex(strings.TrimSuffix(combo, "+"), "+"); split >= 0 {
		modifiers, key = strings.Split(combo[:split], "+"), combo[split+1:]
	}
	var usages []uint8
	shift := false
	for _, name := range modifiers {
		usage, found := phoneModifierUsages[name]
		if !found {
			return nil, E.New("unknown modifier ", strconv.Quote(name), " in ", strconv.Quote(combo), "; modifiers are ", strings.Join(slices.Sorted(maps.Keys(phoneModifierUsages)), ", "))
		}
		shift = shift || usage == usageLeftShift
		usages = append(usages, usage)
	}
	if usage, found := phoneKeyUsages[key]; found {
		return append(usages, usage), nil
	}
	if digits, found := strings.CutPrefix(key, "f"); found {
		if number, err := strconv.Atoi(digits); err == nil && number >= 1 && number <= 12 {
			return append(usages, uint8(0x3a+number-1)), nil
		}
	}
	if len(key) == 1 {
		if stroke, found := asciiKeystrokes[key[0]]; found {
			if stroke.shift && !shift {
				usages = append(usages, usageLeftShift)
			}
			return append(usages, stroke.usage), nil
		}
	}
	return nil, E.New("unknown key ", strconv.Quote(key), " in ", strconv.Quote(combo), "; keys are ", strings.Join(slices.Sorted(maps.Keys(phoneKeyUsages)), ", "), ", f1 to f12 and printable ASCII characters")
}

// dtuhidd hands each event to the keyboard's own queue and clears the events
// not yet handled when it removes the keyboard (DDI 27B5028f dtuhidd
// 0x10003023c).
func pressKeys(ctx context.Context, input *hid.Client, usages []uint8) error {
	for _, usage := range usages {
		if err := input.KeyDown(ctx, usage); err != nil {
			return err
		}
	}
	for _, usage := range slices.Backward(usages) {
		if err := input.KeyUp(ctx, usage); err != nil {
			return err
		}
	}
	return nil
}
