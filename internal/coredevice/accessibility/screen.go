package accessibility

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	E "github.com/sagernet/sing/common/exceptions"
)

type Screen struct {
	BundleID string
	App      Element
	Root     *TreeNode
	System   *TreeNode
	Focused  *ElementRef
	Rounds   []time.Duration
}

const (
	remoteProbeDepth     = 2
	webSearchResultLimit = 10000
)

var webSearch = map[string]any{
	"AXDirection": "AXDirectionNext", "AXResultsLimit": int64(webSearchResultLimit), "AXSearchKey": "AXAnyTypeSearchKey",
	"AXVisibleOnly": true, "AXImmediateDescendantsOnly": false,
}

// After its process was told to end, an application stayed the frontmost one
// for about half a second, answering its bundle ID but no snapshot, until
// SpringBoard was in front (iOS 27.2 24B115).
const frontmostTurnover = time.Second

func untilAnswered(read func() (*Screen, error)) (*Screen, error) {
	deadline := time.Now().Add(frontmostTurnover)
	for {
		screen, err := read()
		if !errors.Is(err, ErrNoAnswer) || time.Now().After(deadline) {
			return screen, err
		}
	}
}

func (c *RemoteClient) ReadScreen(ctx context.Context, springboard uint64, depth int64, overlay func(front Element) bool) (*Screen, error) {
	return untilAnswered(func() (*Screen, error) {
		var rounds []time.Duration
		run := func(batch *Batch) error {
			started := time.Now()
			err := c.Run(ctx, batch)
			rounds = append(rounds, time.Since(started))
			return err
		}
		first := &Batch{}
		front := first.Frontmost(c.display())
		system := first.Application(springboard)
		if err := run(first); err != nil {
			return nil, err
		}
		if err := front.Err("read the frontmost application"); err != nil {
			return nil, err
		}
		if system.Code != 0 {
			return nil, ErrNoApplication
		}
		app := front.Value
		second := &Batch{}
		bundle := second.BundleID(app)
		full := second.Snapshot(app, SnapshotOptions{Depth: depth})
		modal := second.Snapshot(app, SnapshotOptions{Attributes: []int64{nativeClassName}, Depth: depth, HonorModal: true})
		focused := second.Focused(app)
		var systemSnapshot *Result[*Snapshot]
		if system.Value.Ref.PID != app.Ref.PID && (overlay == nil || overlay(app)) {
			systemSnapshot = second.Snapshot(system.Value, SnapshotOptions{Depth: depth})
		}
		if err := run(second); err != nil {
			return nil, err
		}
		if err := bundle.Err("read the frontmost application's bundle ID"); err != nil {
			return nil, c.silent(ctx, app, false, err)
		}
		if err := full.Err("snapshot the frontmost application"); err != nil {
			return nil, c.silent(ctx, app, false, err)
		}
		if err := modal.Err("snapshot the frontmost application's modal view"); err != nil {
			return nil, err
		}
		screen := &Screen{BundleID: bundle.Value, App: app, Root: full.Value.Root}
		if focused.Code == 0 {
			ref := focused.Value.Ref
			screen.Focused = &ref
		}
		markInert(full.Value.Root, modal.Value.Listed, false)
		snapshots := []*Snapshot{full.Value}
		if systemSnapshot != nil {
			if err := systemSnapshot.Err("snapshot SpringBoard"); err != nil {
				return nil, err
			}
			screen.System = systemSnapshot.Value.Root
			snapshots = append(snapshots, systemSnapshot.Value)
		}
		err := c.expand(run, snapshots, springboard, depth)
		screen.Rounds = rounds
		return screen, err
	})
}

var placementAttributes = []int64{nativeAutomationType, nativeTraits, nativeFrame, nativeVisibleFrame, nativeActivation, nativeContextID}

var fieldAttributes = map[string][]int64{
	"class":       {nativeClassName},
	"label":       {nativeLabel},
	"value":       {nativeValue, nativeNumberValue, nativePlaceholder},
	"identifier":  {nativeIdentifier},
	"placeholder": {nativePlaceholder},
	"hint":        {nativeHelp, nativeHint},
	"url":         {nativeURL},
	"expanded":    {nativeExpanded},
	"actions":     {nativeCustomActions},
}

func SnapshotAttributes(fields []string) []int64 {
	attributes := slices.Clone(placementAttributes)
	for _, field := range fields {
		for _, attribute := range fieldAttributes[field] {
			if !slices.Contains(attributes, attribute) {
				attributes = append(attributes, attribute)
			}
		}
	}
	return attributes
}

type ApplicationOptions struct {
	PID        uint64
	Root       *Element
	Clip       *LayoutFrame
	Depth      int64
	Attributes []int64
	Modal      bool
	Focused    bool
	Expand     bool
	Wanted     func(bundleID string) bool
}

var ErrGone = E.New("accessibility: the element is gone")

func (c *RemoteClient) ReadApplication(ctx context.Context, options ApplicationOptions) (*Screen, error) {
	read := func() (*Screen, error) {
		first := &Batch{}
		var app *Result[Element]
		switch {
		case options.Root != nil:
			app = first.Application(options.Root.Ref.PID)
		case options.PID == 0:
			app = first.Frontmost(c.display())
		default:
			app = first.Application(options.PID)
		}
		if err := c.Run(ctx, first); err != nil {
			return nil, err
		}
		if err := app.Err("read the application"); err != nil {
			if options.Root != nil {
				return nil, E.Extend(ErrGone, "read the screen again; its process ", options.Root.Ref.PID, " has no application: ", err)
			}
			return nil, err
		}
		named := options.PID != 0 || options.Root != nil
		var bundle *Result[string]
		if options.Wanted != nil {
			check := &Batch{}
			bundle = check.BundleID(app.Value)
			if err := c.Run(ctx, check); err != nil {
				return nil, err
			}
			if err := bundle.Err("read the application's bundle ID"); err != nil {
				return nil, c.silent(ctx, app.Value, named, err)
			}
			if !options.Wanted(bundle.Value) {
				return &Screen{BundleID: bundle.Value, App: app.Value}, nil
			}
		}
		attributes := options.Attributes
		if options.Expand && len(attributes) > 0 && !slices.Contains(attributes, nativeClassName) {
			attributes = append(slices.Clone(attributes), nativeClassName)
		}
		root := app.Value
		if options.Root != nil {
			root = *options.Root
		}
		second := &Batch{}
		if bundle == nil {
			bundle = second.BundleID(app.Value)
		}
		full := second.Snapshot(root, SnapshotOptions{Attributes: attributes, Depth: options.Depth, Clip: options.Clip})
		var modal *Result[*Snapshot]
		if options.Modal {
			modal = second.Snapshot(root, SnapshotOptions{Attributes: []int64{nativeClassName}, Depth: options.Depth, HonorModal: true})
		}
		var focused *Result[Element]
		if options.Focused {
			focused = second.Focused(app.Value)
		}
		if err := c.Run(ctx, second); err != nil {
			return nil, err
		}
		if err := bundle.Err("read the application's bundle ID"); err != nil {
			return nil, c.silent(ctx, app.Value, named, err)
		}
		if options.Root != nil {
			switch {
			case full.Code != 0:
				return nil, E.Extend(ErrGone, "read the screen again; the snapshot of it, element ", options.Root.Ref.PID, ":", options.Root.Ref.ObjectID,
					", failed: ", full.Err("snapshot the element"))
			case full.Value.Root.Ref != options.Root.Ref:
				return nil, E.Extend(ErrGone, "read the screen again; the snapshot of it, element ", options.Root.Ref.PID, ":", options.Root.Ref.ObjectID,
					", answered element ", full.Value.Root.Ref.PID, ":", full.Value.Root.Ref.ObjectID)
			}
		} else if err := full.Err("snapshot the application"); err != nil {
			return nil, c.silent(ctx, app.Value, named, err)
		}
		screen := &Screen{BundleID: bundle.Value, App: app.Value, Root: full.Value.Root}
		if modal != nil {
			if err := modal.Err("snapshot the application's modal view"); err != nil {
				return nil, err
			}
			markInert(full.Value.Root, modal.Value.Listed, false)
		}
		if focused != nil && focused.Code == 0 {
			ref := focused.Value.Ref
			screen.Focused = &ref
		}
		if options.Expand {
			run := func(batch *Batch) error { return c.Run(ctx, batch) }
			if err := c.expand(run, []*Snapshot{full.Value}, 0, options.Depth); err != nil {
				return nil, err
			}
		}
		return screen, nil
	}
	if options.PID != 0 || options.Root != nil {
		return read()
	}
	return untilAnswered(read)
}

type SilentFrontmostError struct {
	PID uint64
	err error
}

func (e *SilentFrontmostError) Error() string {
	return e.err.Error()
}

func (e *SilentFrontmostError) Unwrap() error {
	return e.err
}

func (c *RemoteClient) silent(ctx context.Context, app Element, check bool, err error) error {
	if !errors.Is(err, ErrNoAnswer) {
		return err
	}
	if check {
		batch := &Batch{}
		front := batch.Frontmost(c.display())
		if c.Run(ctx, batch) != nil || front.Code != 0 || front.Value.Ref.PID != app.Ref.PID {
			return err
		}
	}
	return &SilentFrontmostError{PID: app.Ref.PID, err: err}
}

func markInert(node *TreeNode, listed map[ElementRef]bool, inert bool) {
	node.Inert = inert || !listed[node.Ref]
	for _, child := range node.Children {
		markInert(child, listed, node.Inert)
	}
}

type remoteProbe struct {
	node   *TreeNode
	result *Result[*Snapshot]
}

// A view presenting another process's scene holds a remote element for it
// (24A446 UIKit.axbundle -[_UIScenePresentationViewAccessibility
// _accessibilitySetRemoteElementIfNecessary] 0x189f41d28..0x189f420cc), which
// in SpringBoard is the scene of the application in front.
func (c *RemoteClient) expand(run func(*Batch) error, snapshots []*Snapshot, springboard uint64, depth int64) error {
	probes := &Batch{}
	var remotes []remoteProbe
	for _, snapshot := range snapshots {
		if snapshot.Root.Ref.PID == springboard {
			continue
		}
		for _, node := range snapshot.Remote {
			if !node.Inert {
				remotes = append(remotes, remoteProbe{node, probes.Snapshot(node.Element, SnapshotOptions{Attributes: []int64{nativeClassName}, Depth: remoteProbeDepth})})
			}
		}
	}
	if err := run(probes); err != nil {
		return err
	}
	contents := &Batch{}
	type attachment struct {
		parent *TreeNode
		result *Result[*Snapshot]
	}
	var attachments []attachment
	var webs []*webRead
	for _, probe := range remotes {
		if probe.result.Code != 0 {
			continue
		}
		if area := webArea(probe.result.Value.Root); area != nil {
			webs = append(webs, &webRead{host: probe.node, area: area.Element, search: contents.Search(area.Element, webSearch)})
			continue
		}
		attachments = append(attachments, attachment{probe.node, contents.Snapshot(probe.node.Element, SnapshotOptions{Depth: depth})})
	}
	if err := run(contents); err != nil {
		return err
	}
	for _, item := range attachments {
		if item.result.Code == 0 {
			item.parent.Children = item.result.Value.Root.Children
		}
	}
	return c.readWeb(run, webs)
}

// The snapshot of Safari's remote element answers a
// WKAccessibilityWebPageObject under it, whose child, a
// WebAccessibilityObjectWrapper, holds the page (iOS 27.2 24B115).
func webArea(node *TreeNode) *TreeNode {
	if node.Class == webPageClass {
		for _, child := range node.Children {
			if child.Class == webObjectClass {
				return child
			}
		}
		return nil
	}
	for _, child := range node.Children {
		if area := webArea(child); area != nil {
			return area
		}
	}
	return nil
}

type webRead struct {
	host    *TreeNode
	area    Element
	search  *Result[[]Element]
	order   []ElementRef
	nodes   map[ElementRef]*TreeNode
	parent  map[ElementRef]ElementRef
	missing map[ElementRef]Element
}

func (c *RemoteClient) readWeb(run func(*Batch) error, webs []*webRead) error {
	batch := &Batch{}
	var reads []*Result[loneNode]
	var owners []*webRead
	for _, web := range webs {
		if web.search.Code != 0 {
			continue
		}
		web.nodes, web.parent, web.missing = make(map[ElementRef]*TreeNode), make(map[ElementRef]ElementRef), make(map[ElementRef]Element)
		for _, element := range append([]Element{web.area}, web.search.Value...) {
			if _, seen := web.nodes[element.Ref]; seen {
				continue
			}
			web.nodes[element.Ref] = nil
			web.order = append(web.order, element.Ref)
			reads = append(reads, batch.lone(element))
			owners = append(owners, web)
		}
	}
	for len(batch.requests) > 0 {
		if err := run(batch); err != nil {
			return err
		}
		for index, read := range reads {
			owners[index].add(read)
		}
		batch, reads, owners = &Batch{}, nil, nil
		for _, web := range webs {
			for ref, element := range web.missing {
				delete(web.missing, ref)
				web.nodes[ref] = nil
				reads = append(reads, batch.lone(element))
				owners = append(owners, web)
			}
		}
	}
	for _, web := range webs {
		if web.nodes != nil {
			web.assemble()
		}
	}
	return nil
}

func (w *webRead) add(read *Result[loneNode]) {
	if read.Code != 0 || read.Value.node.Class == webPageClass {
		return
	}
	node, parent := read.Value.node, read.Value.parent
	if w.nodes[node.Ref] == nil {
		w.nodes[node.Ref] = node
	}
	if node.Ref == w.area.Ref || parent == nil || parent.Class == webPageClass {
		return
	}
	w.parent[node.Ref] = parent.Ref
	if _, known := w.nodes[parent.Ref]; !known {
		w.nodes[parent.Ref] = parent
		w.order = append(w.order, parent.Ref)
	}
	if parent.Ref == w.area.Ref || read.Value.grandparent == nil {
		return
	}
	grandparent := *read.Value.grandparent
	w.parent[parent.Ref] = grandparent.Ref
	if _, known := w.nodes[grandparent.Ref]; !known {
		w.missing[grandparent.Ref] = grandparent
	}
}

func (w *webRead) assemble() {
	root := w.nodes[w.area.Ref]
	if root == nil {
		return
	}
	root.Web = true
	if hasArea(root.VisibleFrame) {
		root.Clip = *root.VisibleFrame
	}
	attached := map[ElementRef]bool{w.area.Ref: true}
	var attach func(ref ElementRef) *TreeNode
	attach = func(ref ElementRef) *TreeNode {
		node := w.nodes[ref]
		if node == nil || attached[ref] {
			return node
		}
		attached[ref] = true
		parentRef, found := w.parent[ref]
		parent := root
		if found && parentRef != w.area.Ref {
			if candidate := attach(parentRef); candidate != nil {
				parent = candidate
			}
		}
		node.Web = true
		node.Clip = parent.Clip
		if hasArea(node.VisibleFrame) {
			node.Clip = *node.VisibleFrame
		}
		parent.Children = append(parent.Children, node)
		return node
	}
	for _, ref := range w.order {
		attach(ref)
	}
	w.host.Children = append(w.host.Children, root)
}

type loneNode struct {
	node        *TreeNode
	parent      *TreeNode
	grandparent *Element
}

var loneAttributes = slices.Concat(snapshotAttributes, []any{int64(nativeParent)})

// In a snapshot, an attribute whose value is an element, such as the parent
// (native 5002), comes as that element with its attributes; an element among
// those attributes comes only as a reference (24B5110a UIAccessibility
// __fetchAttributesIfNecessary 0x1bbe141e8, 0x1bbe14260).
func (b *Batch) lone(element Element) *Result[loneNode] {
	parameter := map[string]any{
		"attributes": loneAttributes, "maxDepth": int64(0), "maxChildren": int64(math.MaxInt32), "maxArrayCount": int64(math.MaxInt32),
		"traverseFromParentsToChildren": true, "snapshotKeyHonorModalViews": false,
	}
	result := &Result[loneNode]{}
	b.add(remoteRequest{kind: requestAttribute, object: &element.object, attribute: attributeUserTestingSnapshot, params: map[string]any{"parameter": parameter}},
		func(response remoteResponse, _ *Batch) error {
			result.Code = response.code
			if result.Code != 0 {
				return nil
			}
			if response.value == nil {
				result.Code = errorNoElement
				return nil
			}
			node, _, values, err := snapshotNode(response.value)
			if err != nil {
				return err
			}
			result.Value.node = node
			if values[uint64(nativeParent)] == nil {
				return nil
			}
			parent, _, parentValues, err := snapshotNode(values[uint64(nativeParent)])
			if err != nil {
				return err
			}
			result.Value.parent = parent
			if grandparent, ok := parseTranslation(parentValues[uint64(nativeParent)]); ok {
				element := grandparent.element()
				result.Value.grandparent = &element
			}
			return nil
		})
	return result
}
