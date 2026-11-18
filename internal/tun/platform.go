package tun

import "runtime"

// PlatformName is the current operating system.
func PlatformName() string { return runtime.GOOS }
