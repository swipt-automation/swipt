//go:build android

package android

import (
	"encoding/json"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/fault"
	"github.com/swipt-automation/swipt/internal/selector"
)

type Snapshot struct {
	Windows []*Window `json:"windows"`

	elements map[string]*Element
}

type Window struct {
	Type string   `json:"type"`
	Root *Element `json:"root"`
}

type Element struct {
	Handle             string     `json:"handle"`
	Class              string     `json:"class"`
	PackageName        string     `json:"packageName"`
	Identifier         string     `json:"identifier"`
	Text               string     `json:"text"`
	ContentDescription string     `json:"contentDescription"`
	Hint               string     `json:"hint"`
	StateDescription   string     `json:"stateDescription"`
	Error              string     `json:"error"`
	PaneTitle          string     `json:"paneTitle"`
	TooltipText        string     `json:"tooltipText"`
	OffScreen          bool       `json:"offScreen"`
	Disabled           bool       `json:"disabled"`
	Focused            bool       `json:"focused"`
	Selected           bool       `json:"selected"`
	Secure             bool       `json:"secure"`
	Heading            bool       `json:"heading"`
	Editable           bool       `json:"editable"`
	Checked            *bool      `json:"checked"`
	Actions            []string   `json:"actions"`
	Children           []*Element `json:"children"`

	parent *Element
	window *Window
}

func text(value func(*Element) string) func(*Element) (string, bool) {
	return func(element *Element) (string, bool) {
		text := value(element)
		return text, text != ""
	}
}

var Vocabulary = &selector.Vocabulary[*Element]{
	Fields: []selector.Field[*Element]{
		selector.Text("class", text(func(it *Element) string { return it.Class })),
		selector.Text("packageName", text(func(it *Element) string { return it.PackageName })),
		selector.Text("identifier", text(func(it *Element) string { return it.Identifier })),
		selector.Text("text", text(func(it *Element) string { return it.Text })),
		selector.Text("contentDescription", text(func(it *Element) string { return it.ContentDescription })),
		selector.Text("hint", text(func(it *Element) string { return it.Hint })),
		selector.Text("stateDescription", text(func(it *Element) string { return it.StateDescription })),
		selector.Text("error", text(func(it *Element) string { return it.Error })),
		selector.Text("paneTitle", text(func(it *Element) string { return it.PaneTitle })),
		selector.Text("tooltipText", text(func(it *Element) string { return it.TooltipText })),
		selector.Flag("offScreen", func(it *Element) bool { return it.OffScreen }),
		selector.Flag("disabled", func(it *Element) bool { return it.Disabled }),
		selector.Flag("focused", func(it *Element) bool { return it.Focused }),
		selector.Flag("selected", func(it *Element) bool { return it.Selected }),
		selector.Flag("secure", func(it *Element) bool { return it.Secure }),
		selector.Flag("heading", func(it *Element) bool { return it.Heading }),
		selector.Flag("editable", func(it *Element) bool { return it.Editable }),
		selector.State("checked", func(it *Element) *bool { return it.Checked }),
		selector.Set("actions", func(it *Element) []string { return it.Actions }),
		selector.Text("windowType", text(func(it *Element) string { return it.window.Type })),
	},
	Ref:    func(it *Element) string { return it.Handle },
	Parent: func(it *Element) *Element { return it.parent },
}

func ParseSnapshot(data []byte) (*Snapshot, error) {
	var snapshot Snapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return nil, E.Cause(err, "decode the accessibility snapshot")
	}
	snapshot.elements = make(map[string]*Element)
	var index func(element, parent *Element, window *Window)
	index = func(element, parent *Element, window *Window) {
		element.parent, element.window = parent, window
		snapshot.elements[element.Handle] = element
		for _, child := range element.Children {
			index(child, element, window)
		}
	}
	for _, window := range snapshot.Windows {
		if window.Root != nil {
			index(window.Root, nil, window)
		}
	}
	return &snapshot, nil
}

func (s *Snapshot) Domain(scope string) (domain, universe []*Element, err error) {
	if scope == "" {
		var all []*Element
		for _, window := range s.Windows {
			if window.Root != nil {
				all = append(all, flatten(window.Root)...)
			}
		}
		return all, all, nil
	}
	element := s.elements[scope]
	if element == nil {
		return nil, nil, E.Extend(fault.ErrNotFound, "the element is not in this snapshot")
	}
	return flatten(element)[1:], flatten(element.window.Root), nil
}

func flatten(root *Element) []*Element {
	var out []*Element
	var visit func(element *Element)
	visit = func(element *Element) {
		out = append(out, element)
		for _, child := range element.Children {
			visit(child)
		}
	}
	visit(root)
	return out
}
