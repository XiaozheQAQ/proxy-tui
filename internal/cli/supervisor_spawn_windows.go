//go:build windows

package cli

import "syscall"

// detachedSysProcAttr starts the supervisor without a console and outside the
// client process group so closing the terminal does not terminate it.
func detachedSysProcAttr() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008
	const createNewProcessGroup = 0x00000200
	return &syscall.SysProcAttr{
		CreationFlags: detachedProcess | createNewProcessGroup,
		HideWindow:    true,
	}
}
