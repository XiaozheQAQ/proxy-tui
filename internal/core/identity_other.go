//go:build !windows && !linux && !darwin

package core

import "fmt"

func readProcessIdentity(pid int) (ProcessIdentity, error) {
	return ProcessIdentity{}, fmt.Errorf("%w: unsupported platform for PID %d", ErrProcessIdentityUnavailable, pid)
}
