//go:build android && cgo

package graphics

/*
#cgo LDFLAGS: -lEGL -lGLESv3 -ljnigraphics -lm -lmediandk -lnativewindow
*/
import "C"
