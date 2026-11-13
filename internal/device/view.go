package device

import (
	"bytes"
	"encoding/json"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/coredevice/accessibility"
)

var roles = [...]string{
	"element", "other", "application", "group", "window", "sheet", "drawer", "alert", "dialog",
	"button", "radioButton", "radioGroup", "checkBox", "disclosureTriangle", "popUpButton",
	"comboBox", "menuButton", "toolbarButton", "popover", "keyboard", "key",
	"navigationBar", "tabBar", "tabGroup", "toolbar", "statusBar", "table", "tableRow",
	"tableColumn", "outline", "outlineRow", "browser", "collectionView", "slider",
	"pageIndicator", "progressIndicator", "activityIndicator", "segmentedControl", "picker",
	"pickerWheel", "switch", "toggle", "link", "image", "icon", "searchField",
	"scrollView", "scrollBar", "text", "textField", "secureTextField", "datePicker",
	"textView", "menu", "menuItem", "menuBar", "menuBarItem", "map", "webView",
	"incrementArrow", "decrementArrow", "timeline", "ratingIndicator", "valueIndicator",
	"splitGroup", "splitter", "relevanceIndicator", "colorWell", "helpTag", "matte",
	"dockItem", "ruler", "rulerMarker", "grid", "levelIndicator", "cell", "layoutArea",
	"layoutItem", "handle", "stepper", "tab", "touchBar", "statusItem",
}

// AXRuntime trait bits (24A446 AXRuntime _kAX*Trait).
var traits = [...]string{
	0: "button", 1: "link", 2: "image", 4: "playsSound", 5: "keyboardKey", 6: "staticText", 7: "summaryElement",
	9: "updatesFrequently", 10: "searchField", 11: "startsMediaSession", 12: "adjustable", 13: "allowsDirectInteraction",
	14: "causesPageTurn", 15: "tabBar", 16: "header", 17: "webContent", 18: "textEntry",
}

const (
	traitRadioButton = 1 << 20
	traitTabButton   = 1 << 28
	traitPopUpButton = 1 << 40
	traitToggle      = 1 << 53
)

var traitRoles = []struct{ trait, role string }{
	{"searchField", "searchField"},
	{"textEntry", "textField"},
	{"keyboardKey", "key"},
	{"adjustable", "slider"},
	{"link", "link"},
	{"button", "button"},
	{"image", "image"},
	{"tabBar", "tabBar"},
	{"staticText", "text"},
}

const (
	typeOther    = 1
	typeButton   = 9
	typeKeyboard = 19
)

// XCTest converts an automation type to XCUIElementType with this table (Xcode
// 27.1 27A9275 XCTAutomationSupport _XCTElementTypeFromAXAutomationType
// 0x1e444, 0x46e28).
func elementType(automation uint64) uint64 {
	switch {
	case automation < 8:
		return [...]uint64{1, 2, 4, 5, 6, 3, 7, 8}[automation]
	case automation == 8:
		return 83
	case automation == 83:
		return typeOther
	case automation >= 84 && automation <= 88:
		return typeButton
	case automation > 88:
		return 0
	}
	return automation
}

var structuralRoles = []string{
	"application", "window", "alert", "sheet", "dialog", "popover", "navigationBar", "tabBar", "toolbar",
	"statusBar", "table", "collectionView", "outline", "webView", "keyboard", "menu", "picker",
	"segmentedControl", "tabGroup", "radioGroup",
}

var scrollingRoles = []string{"scrollView", "table", "collectionView", "outline", "webView"}

var (
	contentRoles = []string{"text", "heading", "image"}
	genericRoles = []string{"other", "element", "group"}
)

type ViewElement struct {
	Ref         string         `json:"ref"`
	Role        string         `json:"role"`
	Class       string         `json:"class,omitempty"`
	Traits      []string       `json:"traits,omitempty"`
	Label       string         `json:"label,omitempty"`
	Value       string         `json:"-"`
	ShownValue  *string        `json:"value,omitempty"`
	Identifier  string         `json:"identifier,omitempty"`
	Placeholder string         `json:"placeholder,omitempty"`
	Hint        string         `json:"hint,omitempty"`
	URL         string         `json:"url,omitempty"`
	Frame       *[4]float64    `json:"frame,omitempty"`
	Point       *[2]float64    `json:"point,omitempty"`
	OffScreen   bool           `json:"offScreen,omitempty"`
	Partial     bool           `json:"partial,omitempty"`
	Inert       bool           `json:"inert,omitempty"`
	Remote      bool           `json:"remote,omitempty"`
	Disabled    bool           `json:"disabled,omitempty"`
	Focused     bool           `json:"focused,omitempty"`
	Selected    bool           `json:"selected,omitempty"`
	Expanded    *bool          `json:"expanded,omitempty"`
	Secure      bool           `json:"secure,omitempty"`
	Selection   *[2]uint64     `json:"selection,omitempty"`
	Actions     []string       `json:"actions,omitempty"`
	Truncated   bool           `json:"truncated,omitempty"`
	Children    []*ViewElement `json:"children,omitempty"`

	node   *PhoneNode
	parent *ViewElement
	app    string
}

type View struct {
	App       string       `json:"app"`
	Time      int64        `json:"time"`
	Root      *ViewElement `json:"root"`
	System    *ViewElement `json:"system,omitempty"`
	Truncated bool         `json:"truncated,omitempty"`

	refs     map[string]*ViewElement
	model    *PhoneModel
	raw      bool
	fullOnce sync.Once
	full     *View
}

func refText(ref accessibility.ElementRef) string {
	return strconv.FormatUint(ref.PID, 10) + ":" + strconv.FormatUint(ref.ObjectID, 10)
}

func parseRef(text string) (accessibility.ElementRef, error) {
	pid, objectID, found := strings.Cut(text, ":")
	first, err := strconv.ParseUint(pid, 10, 64)
	if err == nil && found {
		var second uint64
		if second, err = strconv.ParseUint(objectID, 10, 64); err == nil {
			return accessibility.ElementRef{PID: first, ObjectID: second}, nil
		}
	}
	return accessibility.ElementRef{}, E.Extend(ErrInvalid, "the element reference ", strconv.Quote(text), " is not one a read of the screen answered")
}

func NewView(model *PhoneModel, raw bool) *View {
	shapeRoot := func(root *PhoneNode, system bool) *ViewElement {
		if raw {
			return rawElement(root)
		}
		return shape(root, system)[0]
	}
	view := &View{
		App: model.BundleID, Time: time.Now().UnixMilli(), Truncated: len(model.Truncated) > 0, Root: shapeRoot(model.Root, false),
		refs: make(map[string]*ViewElement), model: model, raw: raw,
	}
	if !raw {
		tidy(view.Root, nil)
	}
	view.index(view.Root, nil, model.BundleID)
	if model.System != nil {
		if system := shapeRoot(model.System, true); len(system.Children) > 0 {
			view.System = system
			view.index(system, nil, springboardBundleID)
		}
	}
	return view
}

func (v *View) index(element, parent *ViewElement, app string) {
	element.Ref = refText(element.node.AX.Ref)
	element.parent, element.app = parent, app
	v.refs[element.Ref] = element
	for _, child := range element.Children {
		v.index(child, element, app)
	}
}

func (v *View) matching() *View {
	if v.raw || v.model == nil {
		return v
	}
	v.fullOnce.Do(func() { v.full = NewView(v.model, true) })
	return v.full
}

func (v *View) Element(ref string) *ViewElement {
	return v.refs[ref]
}

func (v *View) all() []*ViewElement {
	out := flatten(v.Root)
	if v.System != nil {
		out = append(out, flatten(v.System)...)
	}
	return out
}

func flatten(root *ViewElement) []*ViewElement {
	var out []*ViewElement
	var visit func(element *ViewElement)
	visit = func(element *ViewElement) {
		out = append(out, element)
		for _, child := range element.Children {
			visit(child)
		}
	}
	visit(root)
	return out
}

// The parts of an element report its custom actions as their own, as those of
// a Home Screen icon, of the Dynamic Island and of Safari's address bar do
// (iOS 27.2 24B115).
func tidy(item *ViewElement, inherited []string) {
	own := item.Actions
	if len(own) > 0 && common.All(own, func(action string) bool { return slices.Contains(inherited, action) }) {
		item.Actions = nil
	}
	if len(own) > 0 {
		inherited = own
	}
	if len(item.Children) > 0 && !item.node.AX.AccessibilityElement && !isControl(item.Role) {
		item.Partial = false
	}
	for _, child := range item.Children {
		tidy(child, inherited)
	}
}

func describe(node *PhoneNode) *ViewElement {
	ax := node.AX
	out := &ViewElement{
		Label: ax.Label, Value: ax.Value, Identifier: ax.Identifier, Placeholder: ax.Placeholder, Hint: ax.Help, URL: ax.URL,
		Remote: node.Remote, Inert: node.Inert, Truncated: node.Truncated, Disabled: !ax.Enabled, Focused: node.Focused, Selected: ax.Selected,
		Expanded: ax.Expanded, Secure: ax.Secure(),
		Actions: common.Uniq(common.Map(ax.Actions, func(action accessibility.CustomAction) string { return action.Name })),
		node:    node,
	}
	for bit, name := range traits {
		if name != "" && ax.Traits&(1<<bit) != 0 {
			out.Traits = append(out.Traits, name)
		}
	}
	out.Role = role(ax, node.Parent == nil && (node.model.read.Within == "" || ax.Application()), out.Traits)
	if ax.Selection != nil {
		out.Selection = &[2]uint64{ax.Selection.Location, ax.Selection.Length}
	}
	// UIKit and WebKit report an empty field's placeholder as its value
	// (24B5110a UIKit.axbundle 0x18a048920, 0x18a04898c; 24B5110a
	// WebCore.axbundle 0x2467137f8, 0x246713ad0).
	if ax.Placeholder != "" && ax.Value == ax.Placeholder {
		out.Value = ""
	} else if out.Value != "" || ax.TextEntry() {
		out.ShownValue = &out.Value
	}
	if node.HasFrame() {
		frame := node.Frame
		out.Frame = new(rectArray(frame))
		if node.Shown() {
			out.Point = new(pointArray(node.Point))
			out.Partial = frame.Width*frame.Height-node.Visible.Width*node.Visible.Height > 1
		} else {
			out.OffScreen = true
		}
	}
	return out
}

// A web check box has the button automation type and the toggle trait, a
// Control Center toggle automation type 0 and the toggle trait, a tab bar's
// tab the button automation type and the tab button trait, and a web pop-up
// button the pop-up button trait without the button trait (iOS and iPadOS
// 27.2 24B115). Headers have the header trait with the StaticText automation
// type, as in Settings and SwiftUI forms, or with automation type 0, as a
// UIKit table's section header (iOS 27.2 24B115).
func role(ax accessibility.Element, application bool, elementTraits []string) string {
	if application {
		return "application"
	}
	name := "other"
	kind := elementType(ax.Type)
	plain := kind == typeButton || kind <= typeOther
	switch {
	case plain && ax.Traits&traitRadioButton != 0:
		name = "radioButton"
	case plain && ax.Traits&traitTabButton != 0:
		name = "tab"
	case plain && ax.Traits&traitToggle != 0:
		name = "toggle"
	case kind > typeOther && kind < uint64(len(roles)):
		name = roles[kind]
	case ax.Traits&traitPopUpButton != 0:
		name = "popUpButton"
	}
	if (name == "other" || name == "text") && slices.Contains(elementTraits, "header") {
		return "heading"
	}
	if name != "other" {
		return name
	}
	for _, it := range traitRoles {
		if slices.Contains(elementTraits, it.trait) {
			return it.role
		}
	}
	return name
}

func rawElement(node *PhoneNode) *ViewElement {
	out := describe(node)
	out.Class = node.AX.Class
	for _, child := range node.Children {
		out.Children = append(out.Children, rawElement(child))
	}
	return out
}

func shape(node *PhoneNode, system bool) []*ViewElement {
	out := describe(node)
	ax := node.AX
	root := node.Parent == nil
	if out.Truncated {
		return []*ViewElement{out}
	}
	if elementType(ax.Type) == typeKeyboard && node.HasFrame() && !node.Shown() {
		return nil
	}
	var children []*ViewElement
	for _, child := range node.Children {
		children = append(children, shape(child, system)...)
	}
	// A table lists its cells in a scroll view of its own, beside its section
	// headers (iOS and iPadOS 27.2 24B115).
	if slices.Contains(scrollingRoles, out.Role) {
		children = common.FlatMap(children, func(child *ViewElement) []*ViewElement {
			if child.Role != "scrollView" || child.Label != "" || child.node.AX.AccessibilityElement || child.Truncated || adds(child, out) {
				return []*ViewElement{child}
			}
			return child.Children
		})
	}
	if ax.AccessibilityElement {
		children = slices.DeleteFunc(children, func(child *ViewElement) bool {
			return !child.node.AX.AccessibilityElement && len(child.Children) == 0 && !child.Focused && !child.Truncated
		})
	}
	if len(children) == 1 && !children[0].Truncated && !root {
		child := children[0]
		switch {
		case out.Label != "" && child.Role != out.Role && (sameText(child.Label, out.Label) || child.Label == "" && sameText(child.Value, out.Label)) &&
			(isControl(out.Role) || isControl(child.Role)):
			switch {
			case isControl(out.Role) && !adds(child, out):
				children = child.Children
			case !adds(out, child):
				out, children = child, child.Children
			}
		case child.Role == out.Role && (out.Label == "" || sameText(child.Label, out.Label)):
			switch {
			case ax.AccessibilityElement && !adds(child, out):
				children = child.Children
			case !ax.AccessibilityElement && !adds(out, child):
				out, children = child, child.Children
			}
		}
	}
	out.Children = children
	ax = out.node.AX
	if len(children) == 0 && out.node.HasFrame() && (ax.Frame.Width == 0 || ax.Frame.Height == 0) && !root {
		return nil
	}
	if ax.AccessibilityElement || root {
		return []*ViewElement{out}
	}
	// SpringBoard shows every application in a card of its switcher, labeled
	// with the application's name, which holds the status bar (iOS 27.2
	// 24B115).
	labeled := (out.Label != "" || out.Identifier != "" && out.Role != "image" || out.Value != "") &&
		!(system && slices.Contains(genericRoles, out.Role))
	content := slices.Contains(contentRoles, out.Role) || labeled && len(children) == 0 && slices.Contains(genericRoles, out.Role)
	container := !content && !isControl(out.Role) || !labeled && len(children) > 0
	switch {
	case len(children) == 0 && (container || !labeled && !isControl(out.Role)):
		return nil
	case slices.Contains(structuralRoles, out.Role), out.Role == "scrollView", out.Role == "cell" && len(children) > 1:
	case container && len(children) == 1 && (children[0].Label == out.Label || out.Label == "") && !adds(out, children[0]):
		return children
	case !labeled && container && !out.Focused && !out.Selected && out.Hint == "" && out.URL == "" && len(out.Actions) == 0:
		return children
	}
	return []*ViewElement{out}
}

func adds(dropped, kept *ViewElement) bool {
	return dropped.Identifier != "" && dropped.Identifier != kept.Identifier || dropped.Value != "" && dropped.Value != kept.Value ||
		dropped.Hint != "" && dropped.Hint != kept.Hint || dropped.URL != "" && dropped.URL != kept.URL ||
		dropped.Selected && !kept.Selected || dropped.Focused && !kept.Focused ||
		!common.All(dropped.Actions, func(action string) bool { return slices.Contains(kept.Actions, action) })
}

// Language & Region labels a row "Number Format, 1, 234, 567.89" and the
// button inside it "Number Format, 1,234,567.89" (iOS 27.0.1 24A446).
func sameText(first, second string) bool {
	plain := func(text string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) || unicode.IsPunct(r) {
				return -1
			}
			return unicode.ToLower(r)
		}, text)
	}
	return first != "" && plain(first) == plain(second)
}

func isControl(role string) bool {
	return !slices.Contains(contentRoles, role) && !slices.Contains(genericRoles, role) &&
		!slices.Contains(structuralRoles, role) && role != "cell" && role != "scrollView"
}

func rectArray(rect Rect) [4]float64 {
	return [4]float64{tenth(rect.X), tenth(rect.Y), tenth(rect.Width), tenth(rect.Height)}
}

func pointArray(point Point) [2]float64 {
	return [2]float64{tenth(point.X), tenth(point.Y)}
}

func tenth(value float64) float64 {
	if rounded := math.Round(value*10) / 10; !math.IsInf(rounded, 0) {
		return rounded
	}
	return value
}

var lineFlags = []struct {
	name string
	set  func(*ViewElement) bool
}{
	{"selected", func(it *ViewElement) bool { return it.Selected }},
	{"focused", func(it *ViewElement) bool { return it.Focused }},
	{"disabled", func(it *ViewElement) bool { return it.Disabled }},
	{"secure", func(it *ViewElement) bool { return it.Secure }},
	{"partial", func(it *ViewElement) bool { return it.Partial }},
	{"offScreen", func(it *ViewElement) bool { return it.OffScreen }},
	{"inert", func(it *ViewElement) bool { return it.Inert }},
	{"remote", func(it *ViewElement) bool { return it.Remote }},
	{"truncated", func(it *ViewElement) bool { return it.Truncated }},
}

func quote(text string) string {
	data, _ := marshalText(text)
	return data
}

func marshalText(value any) (string, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(value)
	return strings.TrimSuffix(out.String(), "\n"), err
}
