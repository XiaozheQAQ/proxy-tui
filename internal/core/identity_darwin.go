//go:build darwin

package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

func readProcessIdentity(pid int) (ProcessIdentity, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.ENOENT) {
			return ProcessIdentity{}, ErrProcessNotFound
		}
		return ProcessIdentity{}, fmt.Errorf("%w: sysctl %d: %v", ErrProcessIdentityUnavailable, pid, err)
	}
	raw, err := unix.SysctlRaw("kern.proc.pathname", pid)
	if err != nil {
		return ProcessIdentity{}, fmt.Errorf("%w: executable path for %d: %v", ErrProcessIdentityUnavailable, pid, err)
	}
	executable := strings.TrimRight(string(raw), "\x00")
	if executable == "" {
		return ProcessIdentity{}, fmt.Errorf("%w: empty executable for %d", ErrProcessIdentityUnavailable, pid)
	}
	start := info.Proc.P_starttime
	return ProcessIdentity{
		PID:        pid,
		StartedAt:  time.Unix(start.Sec, int64(start.Usec)*1000),
		Executable: executable,
	}, nil
}
