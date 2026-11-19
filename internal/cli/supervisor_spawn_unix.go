//go:build !windows

package cli

import "syscall"

// detachedSysProcAttr puts the supervisor in its own session.
func detachedSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
