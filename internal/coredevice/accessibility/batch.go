package accessibility

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strconv"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

type Batch struct {
	requests []remoteRequest
	handlers []func(remoteResponse, *Batch) error
}

type Result[T any] struct {
	Value T
	Code  uint64
}

var ErrNoAnswer = E.New("accessibility: the device answered nothing")

type CodeError struct {
	Operation string
	Code      uint64
}

func (e *CodeError) Error() string {
	return "accessibility: " + e.Operation + ": error " + strconv.FormatUint(e.Code, 10)
}

func (r *Result[T]) Err(operation string) error {
	switch r.Code {
	case 0:
		return nil
	case errorNoElement:
		return E.Extend(ErrNoAnswer, operation)
	}
	return &CodeError{Operation: operation, Code: r.Code}
}

func (b *Batch) add(request remoteRequest, handle func(remoteResponse, *Batch) error) {
	b.requests = append(b.requests, request)
	b.handlers = append(b.handlers, handle)
}

func (c *RemoteClient) Run(ctx context.Context, batch *Batch) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for len(batch.requests) > 0 {
		responses, err := c.exchange(ctx, batch.requests)
		if err != nil {
			return err
		}
		next := &Batch{}
		for index, response := range responses {
			if err = batch.handlers[index](response, next); err != nil {
				return err
			}
		}
		batch = next
	}
	return nil
}

func (c *RemoteClient) Err() error {
	return context.Cause(c.ctx)
}

func (c *RemoteClient) Done() <-chan struct{} {
	return c.ctx.Done()
}

const errorNoElement = math.MaxUint64

func parseApplication(response remoteResponse) (Element, uint64) {
	if app, ok := parseTranslation(response.value); ok && app.application {
		return app.element(), response.code
	}
	return Element{}, cmp.Or(response.code, errorNoElement)
}

// Each display has a frontmost application of its own: with Settings in front
// on the inner display of an unfolded iPhone Duo, display 1, its cover display,
// answers SpringBoard (iPhone19,4 27.2 24B91020).
func (b *Batch) Frontmost(display int) *Result[Element] {
	result := &Result[Element]{}
	b.add(remoteRequest{kind: requestFrontmost, params: map[string]any{"displayId": int64(display)}}, func(response remoteResponse, _ *Batch) error {
		result.Value, result.Code = parseApplication(response)
		return nil
	})
	return result
}

var ErrNoApplication = E.New("accessibility: no application runs in the process")

func (b *Batch) Application(pid uint64) *Result[Element] {
	result := &Result[Element]{}
	b.add(remoteRequest{kind: requestApplication, params: map[string]any{"pid": int64(pid)}}, func(response remoteResponse, _ *Batch) error {
		result.Value, result.Code = parseApplication(response)
		if result.Code == 0 && result.Value.Ref.PID != pid {
			result.Code = errorNoElement
		}
		return nil
	})
	return result
}

func (b *Batch) BundleID(app Element) *Result[string] {
	result := &Result[string]{}
	b.add(remoteRequest{kind: requestAttribute, object: &app.object, attribute: attributeBundleIdentifier}, func(response remoteResponse, _ *Batch) error {
		result.Value, _ = response.value.(string)
		result.Code = response.code
		return nil
	})
	return result
}

func (b *Batch) Focused(app Element) *Result[Element] {
	result := &Result[Element]{}
	b.add(remoteRequest{kind: requestAttribute, object: &app.object, attribute: attributeFocusedElement}, func(response remoteResponse, _ *Batch) error {
		result.Code = response.code
		if focused, ok := parseTranslation(response.value); ok && !focused.application {
			result.Value = focused.element()
		} else if result.Code == 0 {
			result.Code = errorNoElement
		}
		return nil
	})
	return result
}

type SnapshotOptions struct {
	Attributes []int64
	Depth      int64
	HonorModal bool
	Clip       *LayoutFrame
}

func (b *Batch) Snapshot(root Element, options SnapshotOptions) *Result[*Snapshot] {
	attributes := snapshotAttributes
	if len(options.Attributes) > 0 {
		attributes = common.Map(options.Attributes, func(it int64) any { return it })
	}
	parameter := map[string]any{
		"attributes": attributes, "maxDepth": requestDepth(options.Depth), "maxChildren": int64(math.MaxInt32), "maxArrayCount": int64(math.MaxInt32),
		"traverseFromParentsToChildren": true, "snapshotKeyHonorModalViews": options.HonorModal,
	}
	result := &Result[*Snapshot]{}
	b.add(remoteRequest{kind: requestAttribute, object: &root.object, attribute: attributeUserTestingSnapshot, params: map[string]any{"parameter": parameter}},
		func(response remoteResponse, _ *Batch) error {
			result.Code = response.code
			if result.Code != 0 {
				return nil
			}
			if response.value == nil {
				result.Code = errorNoElement
				return nil
			}
			snapshot, err := buildSnapshot(response.value, options.Depth, options.Clip)
			if err != nil {
				return err
			}
			result.Value = snapshot
			return nil
		})
	return result
}

// The attributes of a batch share one error: an attribute that fails can set
// it for the whole batch, or only drop out of it (24A446
// AccessibilityPlatformTranslation -[AXPTranslator_iOS
// processMultipleAttributeRequest:removeEmptyValue:] 0x24fe47af4, 0x24fe47b34,
// 0x24fe47b58).
func (b *Batch) Attributes(element Element) *Result[Element] {
	result := &Result[Element]{}
	b.add(remoteRequest{kind: requestAttributes, object: &element.object, params: map[string]any{"attributes": elementAttributes}},
		func(response remoteResponse, next *Batch) error {
			if values, ok := response.value.(map[any]any); response.code == 0 && ok {
				result.Value = parseElement(element.object, values)
				return nil
			}
			values := make(map[any]any)
			unanswered := make(map[uint64]uint64)
			pending := len(elementAttributes)
			for _, attribute := range elementAttributes {
				attribute := uint64(attribute.(int64))
				next.add(remoteRequest{kind: requestAttribute, object: &element.object, attribute: attribute}, func(single remoteResponse, _ *Batch) error {
					switch {
					case single.code != 0:
						unanswered[attribute] = single.code
					case single.value != nil:
						values[attribute] = single.value
					}
					pending--
					if pending > 0 {
						return nil
					}
					result.Value = parseElement(element.object, values)
					result.Value.unanswered = unanswered
					if len(unanswered) == len(elementAttributes) {
						result.Code = cmp.Or(response.code, errorNoElement)
					}
					return nil
				})
			}
			return nil
		})
	return result
}

func (b *Batch) Parent(element Element) *Result[Element] {
	result := &Result[Element]{}
	b.add(remoteRequest{kind: requestAttribute, object: &element.object, attribute: attributeNative, params: map[string]any{"parameter": int64(nativeParent)}},
		func(response remoteResponse, _ *Batch) error {
			result.Code = response.code
			if parent, ok := parseTranslation(response.value); ok {
				result.Value = parent.element()
			} else if result.Code == 0 {
				result.Code = errorNoElement
			}
			return nil
		})
	return result
}

func (b *Batch) Native(element Element, native uint64) *Result[any] {
	result := &Result[any]{}
	b.add(remoteRequest{kind: requestAttribute, object: &element.object, attribute: attributeNative, params: map[string]any{"parameter": int64(native)}},
		func(response remoteResponse, _ *Batch) error {
			result.Value, result.Code = response.value, response.code
			return nil
		})
	return result
}

// _accessibilityAnimationsInProgress reads a count of the running UIView
// animations of the process, whichever element is asked (24A446 UIAccessibility
// 0x1bb749c9c; 24A446 UIKit.axbundle -[UIViewAnimationStateAccessibility
// animationDidStart:] 0x189f41c70).
func (b *Batch) AnimationsInProgress(element Element) *Result[bool] {
	result := &Result[bool]{}
	b.add(remoteRequest{kind: requestAttribute, object: &element.object, attribute: attributeNative, params: map[string]any{"parameter": int64(nativeAnimationsInProgress)}},
		func(response remoteResponse, _ *Batch) error {
			result.Value, _ = response.value.(bool)
			result.Code = response.code
			return nil
		})
	return result
}

// Once the translator holds more than 4096 objects, the next new one drops the
// 1024 added first. A dropped one is looked up as pid 0 and ID 0 (24B5110a
// AccessibilityPlatformTranslation 0x253fd75a0, 0x253fe32d8), so it answers
// all-zero element data, while actions on it report success and do nothing
// (iPadOS 27.2 24B115).
func (b *Batch) Mapped(element Element) *Result[bool] {
	result := &Result[bool]{}
	b.add(remoteRequest{kind: requestAttribute, object: &element.object, attribute: attributeRawElementData}, func(response remoteResponse, _ *Batch) error {
		result.Code = response.code
		result.Value = slices.ContainsFunc(decodeLayoutRawElement(response.value), func(octet byte) bool { return octet != 0 })
		return nil
	})
	return result
}

// Attribute 113 runs native 92516, which a WebCore element answers with
// accessibilityFindMatchingObjects:, a depth-first search in document order
// (24B5110a AccessibilityPlatformTranslation 0x253fef320; 24B5110a
// WebCore.axbundle 0x246716324; WebKit-7625.2.8.11.9
// Source/WebCore/accessibility/AXSearchManager.cpp:410). AXVisibleOnly tests
// an object only against its scrollable ancestors (WebKit-7625.2.8.11.9
// Source/WebCore/accessibility/AccessibilityObject.cpp:3927).
func (b *Batch) Search(root Element, parameters map[string]any) *Result[[]Element] {
	result := &Result[[]Element]{}
	b.add(remoteRequest{kind: requestAttribute, object: &root.object, attribute: attributeSearch, params: map[string]any{"parameter": parameters}},
		func(response remoteResponse, _ *Batch) error {
			result.Code = response.code
			values, _ := response.value.([]any)
			for _, value := range values {
				if found, ok := parseTranslation(value); ok {
					result.Value = append(result.Value, found.element())
				}
			}
			return nil
		})
	return result
}

// The system-wide hit test asks backboardd for the context on top at a point
// of the display (24A446 BackBoard.axbundle 0x22a9acfc4), in points of its
// native orientation (iPadOS 27.2 24B115).
func (b *Batch) HitTest(point LayoutPoint, display int) *Result[Element] {
	result := &Result[Element]{}
	b.add(remoteRequest{kind: requestHitTest, params: map[string]any{"point": point.archiveObject(), "displayId": int64(display), "contextId": int64(0)}},
		func(response remoteResponse, _ *Batch) error {
			result.Code = response.code
			if hit, ok := parseTranslation(response.value); ok {
				result.Value = hit.element()
			} else if result.Code == 0 {
				result.Code = errorNoElement
			}
			return nil
		})
	return result
}

func (b *Batch) Perform(element Element, action uint64) *Result[bool] {
	result := &Result[bool]{}
	b.add(remoteRequest{kind: requestAction, object: &element.object, action: action}, func(response remoteResponse, _ *Batch) error {
		result.Value, _ = response.value.(bool)
		result.Code = response.code
		return nil
	})
	return result
}

// Request type 9 runs processSupportedActions:, which answers those of
// parameters["actions"] whose support check passes, and none for none asked
// (24A446 AccessibilityPlatformTranslation 0x24fe57c24,
// 0x24fe4dc18..0x24fe4ddb0).
const requestSupportedActions = 9

func (b *Batch) SupportedActions(element Element, actions []uint64) *Result[[]uint64] {
	result := &Result[[]uint64]{}
	candidates := common.Map(actions, func(it uint64) any { return int64(it) })
	b.add(remoteRequest{kind: requestSupportedActions, object: &element.object, params: map[string]any{"actions": candidates}}, func(response remoteResponse, _ *Batch) error {
		result.Code = response.code
		values, _ := response.value.([]any)
		for _, value := range values {
			if action, ok := unsigned(value); ok {
				result.Value = append(result.Value, action)
			}
		}
		return nil
	})
	return result
}

// AXP action 4 passes AXPCustomActionIdentifier to performAction:withValue:
// (24A446 AccessibilityPlatformTranslation 0x24fe48264).
func (b *Batch) PerformCustom(element Element, action CustomAction) *Result[bool] {
	result := &Result[bool]{}
	b.add(remoteRequest{kind: requestAction, object: &element.object, action: actionCustom, params: map[string]any{"AXPCustomActionIdentifier": action.Identifier}},
		func(response remoteResponse, _ *Batch) error {
			result.Value, _ = response.value.(bool)
			result.Code = response.code
			return nil
		})
	return result
}

// Request type 8 runs processSetAttribute:, which sets attribute 53 from
// parameters["value"] with setAXAttribute:2006 withString: (24A446
// AccessibilityPlatformTranslation 0x24fe57b48, 0x24fe4d8cc..0x24fe4d90c).
const requestSet = 8

func (b *Batch) set(element Element, attribute uint64, params map[string]any) *Result[struct{}] {
	result := &Result[struct{}]{}
	b.add(remoteRequest{kind: requestSet, object: &element.object, attribute: attribute, params: params}, func(response remoteResponse, _ *Batch) error {
		result.Code = response.code
		return nil
	})
	return result
}

func (b *Batch) SetValue(element Element, text string) *Result[struct{}] {
	return b.set(element, attributeValue, map[string]any{"value": text})
}
