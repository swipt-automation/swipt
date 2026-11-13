//go:build !windows

package usbmuxd

const defaultNetwork, defaultAddress = "unix", "/var/run/usbmuxd"
