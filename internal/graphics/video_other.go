//go:build cgo && !darwin && !android

package graphics

func newLiveFrames(*screenStream) (liveFrames, error) {
	return nil, unsupported("screen frames")
}
