//go:build cgo && (darwin || android)

package graphics

import "github.com/swipt-automation/swipt/internal/screen"

func (s *screenStream) deliver(next *imageNode) {
	s.access.Lock()
	if s.closed || s.released || s.failure != nil || s.undecodable != nil {
		s.access.Unlock()
		next.release()
		return
	}
	previous := s.latest
	s.latest = next
	s.sequence++
	close(s.changed)
	s.changed = make(chan struct{})
	s.access.Unlock()
	if previous != nil {
		supersede(previous)
	}
}

func frameNode(native nativeFrame, info screen.Info, turns int, time, timestamp float64) *imageNode {
	source := &frameSource{native: native, width: info.Width, height: info.Height, turns: turns & 3}
	if source.width <= 0 || source.height <= 0 {
		source.width, source.height = native.size()
	}
	width, height := source.width, source.height
	if source.turns%2 == 1 {
		width, height = height, width
	}
	scale := info.Scale
	if !(scale > 0) {
		scale = 1
	}
	return newImageNode(width, height, scaling(1/scale, 1/scale), time, timestamp, source)
}

func supersede(node *imageNode) {
	node.access.Lock()
	source, isFrame := node.source.(*frameSource)
	shared := node.references > 1
	node.access.Unlock()
	if isFrame && shared {
		source.native.detach()
	}
	node.release()
}
