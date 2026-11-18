package app

import "runtime"

func osName() string { return runtime.GOOS }

func osArch() string { return runtime.GOARCH }
