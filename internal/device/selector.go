package device

import (
	"strings"
	"unicode"

	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/fault"
	"github.com/swipt-automation/swipt/internal/selector"
)

type Selector = selector.Selector[*ViewElement]

var Vocabulary = &selector.Vocabulary[*ViewElement]{
	Fields: []selector.Field[*ViewElement]{
		selector.Text("role", func(it *ViewElement) (string, bool) { return it.Role, true }),
		selector.Text("class", func(it *ViewElement) (string, bool) { return it.Class, it.Class != "" }),
		selector.Text("label", func(it *ViewElement) (string, bool) { return it.Label, it.Label != "" }),
		selector.Text("value", func(it *ViewElement) (string, bool) {
			if it.ShownValue == nil {
				return "", false
			}
			return *it.ShownValue, true
		}),
		selector.Text("identifier", func(it *ViewElement) (string, bool) { return it.Identifier, it.Identifier != "" }),
		selector.Text("placeholder", func(it *ViewElement) (string, bool) { return it.Placeholder, it.Placeholder != "" }),
		selector.Text("hint", func(it *ViewElement) (string, bool) { return it.Hint, it.Hint != "" }),
		selector.Text("url", func(it *ViewElement) (string, bool) { return it.URL, it.URL != "" }),
		selector.Text("app", func(it *ViewElement) (string, bool) { return it.app, true }),
		selector.Set("traits", func(it *ViewElement) []string { return it.Traits }),
		selector.Flag("selected", func(it *ViewElement) bool { return it.Selected }),
		selector.Flag("focused", func(it *ViewElement) bool { return it.Focused }),
		selector.Flag("disabled", func(it *ViewElement) bool { return it.Disabled }),
		selector.Flag("secure", func(it *ViewElement) bool { return it.Secure }),
		selector.Flag("partial", func(it *ViewElement) bool { return it.Partial }),
		selector.Flag("offScreen", func(it *ViewElement) bool { return it.OffScreen }),
		selector.Flag("inert", func(it *ViewElement) bool { return it.Inert }),
		selector.Flag("remote", func(it *ViewElement) bool { return it.Remote }),
		selector.Flag("truncated", func(it *ViewElement) bool { return it.Truncated }),
		selector.State("expanded", func(it *ViewElement) *bool { return it.Expanded }),
	},
	Ref:    func(it *ViewElement) string { return it.Ref },
	Parent: func(it *ViewElement) *ViewElement { return it.parent },
	Plain: func(text string) string {
		return strings.TrimFunc(PlainText(text), func(r rune) bool { return unicode.IsSpace(r) || r == '\uFEFF' })
	},
	Repeats: func(element, ancestor *ViewElement) bool {
		return element.Label != "" && sameText(ancestor.Label, element.Label)
	},
}

func (v *View) Domain(scope string) (domain, universe []*ViewElement, err error) {
	v = v.matching()
	if scope == "" {
		all := v.all()
		return all, all, nil
	}
	element := v.refs[scope]
	if element == nil {
		return nil, nil, E.Extend(fault.ErrNotFound, "the element is not in this read of the screen")
	}
	root := element
	for root.parent != nil {
		root = root.parent
	}
	return flatten(element)[1:], flatten(root), nil
}
