//go:build android && cgo

package android

// #cgo LDFLAGS: -landroid
import "C"
