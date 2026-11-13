package accessibility

import (
	E "github.com/sagernet/sing/common/exceptions"
)

// axauditd parses the application's answer as a binary plist, and answers nil
// when an object in it nests 129 deep (24C5037i CoreFoundation
// ___CFBinaryPlistCreateObjectFiltered 0x18061e414; 24C5037i AXRuntime
// _AXUnserializeCFType3 0x18fabd614). A snapshot nests each level 2 deeper,
// and the values of a node 3 or more deeper than the node (24C5037i AXRuntime
// 0x18fb01858, 0x18fb01cf8).
const (
	DefaultDepth = 50
	MaxDepth     = 56
)

// maxDepth counts the root as the first level (24C5037i UIAccessibility
// 0x1bc7217f8, 0x1bc721b68).
func requestDepth(depth int64) int64 {
	return depth + 1
}

// A snapshot gives every node it reaches its number of children, also a node
// at maxDepth, whose children it leaves empty (24C5037i UIAccessibility
// 0x1bc721b38, 0x1bc721b68, 0x1bc721908).
func cutOff(fields map[string]any, children []any) bool {
	count, _ := unsigned(fields["UIAccessibilitySnapshotKeyChildrenCount"])
	return count > 0 && len(children) == 0
}

type Snapshot struct {
	Root   *TreeNode
	Remote []*TreeNode
	Listed map[ElementRef]bool
}

func snapshotNode(value any) (*TreeNode, map[string]any, map[any]any, error) {
	fields, _ := value.(map[string]any)
	object, ok := parseTranslation(fields["UIAccessibilitySnapshotKeyElement"])
	if !ok {
		return nil, nil, nil, E.New("accessibility: snapshot: a node has no element")
	}
	values, _ := fields["UIAccessibilitySnapshotKeyAttributes"].(map[any]any)
	return &TreeNode{Element: parseSnapshotElement(object, values)}, fields, values, nil
}

func buildSnapshot(value any, depth int64, area *LayoutFrame) (*Snapshot, error) {
	seen := make(map[ElementRef]bool)
	var build func(value any, level int64) (*TreeNode, error)
	build = func(value any, level int64) (*TreeNode, error) {
		node, fields, _, err := snapshotNode(value)
		if err != nil {
			return nil, err
		}
		if seen[node.Ref] {
			return nil, nil
		}
		seen[node.Ref] = true
		children, _ := fields["UIAccessibilitySnapshotKeyChildren"].([]any)
		for _, value := range children {
			child, err := build(value, level+1)
			if err != nil {
				return nil, err
			}
			if child != nil {
				node.Children = append(node.Children, child)
			}
		}
		node.Truncated = level == depth && cutOff(fields, children)
		return node, nil
	}
	root, err := build(value, 0)
	if err != nil {
		return nil, err
	}
	out := &Snapshot{Root: root, Listed: seen}
	if area == nil {
		area = root.Frame
	}
	out.clip(root, area)
	return out, nil
}

func (s *Snapshot) clip(node *TreeNode, area *LayoutFrame) {
	if area != nil {
		node.Clip = *area
	}
	switch {
	case hasArea(node.VisibleFrame):
		node.Clip = *node.VisibleFrame
	// A window's frame is in its own space, which differs from the
	// application's for SpringBoard: its application element is landscape
	// on a landscape iPad, while its windows and their content are portrait
	// (iPadOS 27.2 24B115).
	case node.Type == automationWindow && hasArea(node.Frame):
		node.Clip = *node.Frame
	}
	if node.Class == remoteElementClass {
		s.Remote = append(s.Remote, node)
	}
	for _, child := range node.Children {
		s.clip(child, &node.Clip)
	}
}

func hasArea(frame *LayoutFrame) bool {
	return frame != nil && frame.Width > 0 && frame.Height > 0
}
