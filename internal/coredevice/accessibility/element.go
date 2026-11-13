package accessibility

import (
	"fmt"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
)

const (
	attributeActivationPoint   = 1
	attributeContextID         = 12
	attributeHelp              = 23
	attributeIsElement         = 26
	attributeIsFocused         = 28
	attributeIsSelected        = 30
	attributePlaceholder       = 42
	attributeSelectedTextRange = 47
	attributeValue             = 53
	attributeRawValue          = 54
	attributeAutomationType    = 61
	attributeSearch            = 113
	attributeExpanded          = 119
	attributeURL               = 120
)

// Attribute 52 passes parameters["parameter"] to the application's
// _accessibilityUserTestingSnapshotWithOptions:, which answers the automation
// hierarchy in one response, and nil unless attributes is an array holding a
// number and maxDepth, maxChildren, maxArrayCount and
// traverseFromParentsToChildren are numbers (24B5110a
// AccessibilityPlatformTranslation 0x253fef138; 24B5110a
// UIAccessibility 0x1bbe13dbc..0x1bbe13e6c; 24A446 UIAccessibility
// 0x1bb7679f4, 0x1bb76625c). Asked to honor modal views, it keeps among
// siblings the last modal view and the views
// _accessibilityObscuredScreenAllowedViews names (24B5110a UIAccessibility
// 0x1bbe129f0, 0x1bbe12a08, 0x1bbe12aa0). It does not reach into other
// processes: their content is an element of class AXRemoteElement with no
// children (iOS 27.2 24B115).
const attributeUserTestingSnapshot = 52

// UIAccessibility native attributes (24A446 UIAccessibility
// -[NSObject(AXPrivCategory) _iosAccessibilityAttributeValue:] 0x1bb739f00,
// table 0x1bb70ca6c).
const (
	nativeLabel                = 2001
	nativeHint                 = 2002
	nativeFrame                = 2003
	nativeTraits               = 2004
	nativeSelectedRange        = 2005
	nativeValue                = 2006
	nativeActivation           = 2007
	nativeIsElement            = 2016
	nativeURL                  = 2020
	nativeContextID            = 2021
	nativeCustomActions        = 2036
	nativeVisibleFrame         = 2057
	nativeExpanded             = 2114
	nativeHelp                 = 2188
	nativeParent               = 5002
	nativeAnimationsInProgress = 5028
	nativeIdentifier           = 5019
	nativePlaceholder          = 5043
	nativeAutomationType       = 5058
	nativeNumberValue          = 5059
)

// axauditd converts the snapshot's values for the host: elements to
// translation objects, AXValues to NSValues, arrays and dictionaries by their
// contents, and strings, attributed strings, numbers, URLs, data, dates and a
// few other Foundation and CoreGraphics types as they are; any other type
// raises (24B5110a AccessibilityPlatformTranslation
// __AXPConvertOutgoingValueWithDesiredType 0x253fdb2a0, raise 0x253fdb8c8).
var snapshotAttributes = []any{
	int64(nativeAutomationType), int64(nativeClassName), int64(nativeIsElement), int64(nativeLabel), int64(nativeValue),
	int64(nativeNumberValue), int64(nativeIdentifier), int64(nativePlaceholder), int64(nativeTraits), int64(nativeFrame),
	int64(nativeActivation), int64(nativeContextID), int64(nativeExpanded), int64(nativeCustomActions), int64(nativeHelp),
	int64(nativeHint), int64(nativeURL), int64(nativeSelectedRange), int64(nativeVisibleFrame),
}

const (
	remoteElementClass = "AXRemoteElement"
	webPageClass       = "WKAccessibilityWebPageObject"
	webObjectClass     = "WebAccessibilityObjectWrapper"
)

// XCTest converts automation type 2 to XCUIElementTypeWindow, and 45, 49, 50
// and 52 to XCUIElementTypeSearchField, TextField, SecureTextField and
// TextView (Xcode 27.1 27A9275 XCTAutomationSupport
// _XCTElementTypeFromAXAutomationType 0x1e444, table 0x46e28).
const (
	automationWindow          = 2
	automationSearchField     = 45
	automationTextField       = 49
	automationSecureTextField = 50
	automationTextView        = 52
)

// AXPAction numbers (24A446 AccessibilityPlatformTranslation, _AXPActionToString
// 0x24fe585b8, -[AXPTranslator_iOS processActionRequest:] 0x24fe4807c).
const (
	ActionDecrement         = 1
	ActionEscape            = 2
	ActionIncrement         = 3
	actionCustom            = 4
	ActionPress             = 5
	ActionScrollDownByPage  = 6
	ActionScrollLeftByPage  = 7
	ActionScrollRightByPage = 8
	ActionScrollToVisible   = 11
	ActionScrollUpByPage    = 12
	ActionShowContextMenu   = 13
)

var elementAttributes = []any{
	int64(attributeLabel), int64(attributeValue), int64(attributeIdentifier), int64(attributeFrame),
	int64(attributeActivationPoint), int64(attributeTraits), int64(attributeIsEnabled), int64(attributeIsFocused),
	int64(attributeIsSelected), int64(attributeCustomActions), int64(attributePlaceholder), int64(attributeAutomationType),
	int64(attributeExpanded), int64(attributeContextID), int64(attributeHelp), int64(attributeURL),
	int64(attributeSelectedTextRange), int64(attributeIsElement), int64(attributeRawValue),
}

type ElementRef struct {
	PID      uint64
	ObjectID uint64
}

type TextRange struct {
	Location, Length uint64
}

type CustomAction struct {
	Name       string
	Identifier string
}

type Element struct {
	Ref                  ElementRef
	Type                 uint64
	AccessibilityElement bool
	Class                string
	Label                string
	Value                string
	// A web radio button reports 0 as its value and its state as the raw
	// value, "1" when checked (iPadOS 27.2 24B115).
	RawValue    string
	Identifier  string
	Placeholder string
	Frame       *LayoutFrame
	// Native 2057 cuts Frame to the window, to what the software keyboard
	// leaves, and to the bounds less the content insets of each scroll view
	// holding it (24A446 UIAccessibility -[NSObject(AXPrivCategory)
	// _accessibilityVisibleFrame:] 0x1bb746ed0; 24A446 UIKit.axbundle
	// -[UIScrollViewAccessibility _accessibilityContentFrame] 0x189ffa834).
	VisibleFrame *LayoutFrame
	Activation   *LayoutPoint
	Traits       uint64
	Enabled      bool
	Focused      bool
	Selected     bool
	Expanded     *bool
	Actions      []CustomAction
	Help         string
	URL          string
	Selection    *TextRange
	Context      uint64

	object     translationObject
	unanswered map[uint64]uint64
}

type TreeNode struct {
	Element
	Children  []*TreeNode
	Clip      LayoutFrame
	Inert     bool
	Web       bool
	Truncated bool
}

func (e Element) Application() bool {
	return e.object.application
}

func (t translationObject) ref() ElementRef {
	return ElementRef{PID: t.pid, ObjectID: t.objectID}
}

func (t translationObject) element() Element {
	return Element{Ref: t.ref(), object: t}
}

func parseElement(object translationObject, values map[any]any) Element {
	element := Element{
		Ref:                  object.ref(),
		AccessibilityElement: truthy(values[uint64(attributeIsElement)]),
		Label:                textValue(values[uint64(attributeLabel)]),
		Value:                textValue(values[uint64(attributeValue)]),
		RawValue:             textValue(values[uint64(attributeRawValue)]),
		Identifier:           textValue(values[uint64(attributeIdentifier)]),
		Placeholder:          textValue(values[uint64(attributePlaceholder)]),
		Frame:                decodeLayoutFrame(values[uint64(attributeFrame)]),
		Activation:           decodeLayoutPoint(values[uint64(attributeActivationPoint)]),
		Enabled:              truthy(values[uint64(attributeIsEnabled)]),
		Focused:              truthy(values[uint64(attributeIsFocused)]),
		Selected:             truthy(values[uint64(attributeIsSelected)]),
		Help:                 textValue(values[uint64(attributeHelp)]),
		URL:                  urlValue(values[uint64(attributeURL)]),
		Actions:              customActions(values[uint64(attributeCustomActions)], "AXPCustomActionName", "AXPCustomActionIdentifier"),
		object:               object,
	}
	element.Type, _ = unsigned(values[uint64(attributeAutomationType)])
	element.Traits, _ = unsigned(values[uint64(attributeTraits)])
	element.Context, _ = unsigned(values[uint64(attributeContextID)])
	// WebKit answers location 0 for an element with no selection (24B5110a
	// WebCore.axbundle 0x24670da3c).
	if element.TextEntry() {
		element.Selection = decodeTextRange(values[uint64(attributeSelectedTextRange)])
	}
	// Attribute 119 answers 1 when expanded, 0 when collapsed, and error 3 for
	// an element with no expanded state (24B5110a
	// AccessibilityPlatformTranslation 0x253fda430..0x253fda578).
	if value := values[uint64(attributeExpanded)]; value != nil {
		expanded := truthy(value)
		element.Expanded = &expanded
	}
	return element
}

// AXP attribute 13 answers dictionaries keyed AXPCustomActionName and
// AXPCustomActionIdentifier, translated from those of native 2036, keyed
// CustomActionName, an attributed string, and CustomActionIdentifier
// (24B5110a AccessibilityPlatformTranslation 0x253fde78c,
// 0x253fda588..0x253fda6e0; 24A446 UIAccessibility -[NSObject(AXPrivCategory)
// _accessibilityCustomActionNamesAndIdentifiers] 0x1bb734074).
func customActions(value any, nameKey, identifierKey string) []CustomAction {
	var out []CustomAction
	actions, _ := value.([]any)
	for _, value := range actions {
		action, ok := value.(map[string]any)
		if !ok {
			continue
		}
		identifier, _ := action[identifierKey].(string)
		if identifier != "" {
			out = append(out, CustomAction{Name: textValue(action[nameKey]), Identifier: identifier})
		}
	}
	return out
}

// XCTest reads enabled and selected from the traits (Xcode 27.1 27A9275
// XCTAutomationSupport -[XCElementSnapshot isEnabled] 0x25118,
// -[XCElementSnapshot isSelected] 0x251d4), and AXP attribute 23 the hint from
// native 2188, or 2002 when that is empty (24B5110a
// AccessibilityPlatformTranslation 0x253fdbd14).
func parseSnapshotElement(object translationObject, values map[any]any) Element {
	element := Element{
		Ref:                  object.ref(),
		AccessibilityElement: truthy(values[uint64(nativeIsElement)]),
		Class:                textValue(values[uint64(nativeClassName)]),
		Label:                textValue(values[uint64(nativeLabel)]),
		Value:                textValue(values[uint64(nativeValue)]),
		RawValue:             textValue(values[uint64(nativeNumberValue)]),
		Identifier:           textValue(values[uint64(nativeIdentifier)]),
		Placeholder:          textValue(values[uint64(nativePlaceholder)]),
		Frame:                decodeLayoutFrame(values[uint64(nativeFrame)]),
		VisibleFrame:         decodeLayoutFrame(values[uint64(nativeVisibleFrame)]),
		Activation:           decodeLayoutPoint(values[uint64(nativeActivation)]),
		Help:                 textValue(values[uint64(nativeHelp)]),
		URL:                  urlValue(values[uint64(nativeURL)]),
		Actions:              customActions(values[uint64(nativeCustomActions)], "CustomActionName", "CustomActionIdentifier"),
		object:               object,
	}
	if element.Help == "" {
		element.Help = textValue(values[uint64(nativeHint)])
	}
	element.Type, _ = unsigned(values[uint64(nativeAutomationType)])
	element.Traits, _ = unsigned(values[uint64(nativeTraits)])
	element.Context, _ = unsigned(values[uint64(nativeContextID)])
	element.Enabled = element.Traits&traitNotEnabled == 0
	element.Selected = element.Traits&traitSelected != 0
	if element.TextEntry() {
		element.Selection = decodeTextRange(values[uint64(nativeSelectedRange)])
	}
	switch status, _ := unsigned(values[uint64(nativeExpanded)]); status {
	case 1, 2:
		expanded := status == 1
		element.Expanded = &expanded
	}
	return element
}

func (e Element) TextEntry() bool {
	return e.Traits&traitTextEntry != 0
}

func (e Element) TextInput() bool {
	switch e.Type {
	case automationSearchField, automationTextField, automationSecureTextField, automationTextView:
		return true
	}
	return e.TextEntry() || e.Secure()
}

func (e Element) TextView() bool {
	return e.Type == automationTextView
}

func (e Element) TextErr() error {
	if code, failed := e.unanswered[attributeValue]; failed {
		return E.New("accessibility: read the text: error ", code)
	}
	return nil
}

func (e Element) Secure() bool {
	return e.Type == automationSecureTextField || e.Traits&traitSecureTextField != 0
}

func textValue(value any) string {
	switch value := value.(type) {
	case string:
		return value
	case map[string]any:
		text, _ := value["NSString"].(string)
		return text
	case bool:
		if value {
			return "1"
		}
		return "0"
	case uint64:
		return strconv.FormatUint(value, 10)
	case int64:
		return strconv.FormatInt(value, 10)
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64)
	}
	return ""
}

func truthy(value any) bool {
	if flag, ok := value.(bool); ok {
		return flag
	}
	number, ok := unsigned(value)
	return ok && number != 0
}

func urlValue(value any) string {
	if fields, ok := value.(map[string]any); ok && fields["$class"] == "NSURL" {
		return textValue(fields["NS.relative"])
	}
	return ""
}

func decodeTextRange(value any) *TextRange {
	fields, ok := value.(map[string]any)
	if !ok || fields["$class"] != "NSValue" {
		return nil
	}
	location, locationOK := unsigned(fields["NS.rangeval.location"])
	length, lengthOK := unsigned(fields["NS.rangeval.length"])
	if !locationOK || !lengthOK {
		return nil
	}
	return &TextRange{Location: location, Length: length}
}

func decodeLayoutPoint(value any) *LayoutPoint {
	fields, ok := value.(map[string]any)
	if !ok || fields["$class"] != "NSValue" {
		return nil
	}
	s, ok := fields["NS.pointval"].(string)
	if !ok {
		return nil
	}
	var point LayoutPoint
	if _, err := fmt.Sscanf(s, "{%f, %f}", &point.X, &point.Y); err != nil {
		return nil
	}
	return &point
}
