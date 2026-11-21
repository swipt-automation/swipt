//go:build cgo && !darwin && !android

package graphics

type screenRecording struct {
	directory string
	manifest  *recordingManifest
}

func newScreenRecording(directory string, manifest *recordingManifest) *screenRecording {
	return &screenRecording{directory: directory, manifest: manifest}
}
