//go:build windows

package core

import (
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
)

func readProcessIdentity(pid int) (ProcessIdentity, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return ProcessIdentity{}, ErrProcessNotFound
		}
		return ProcessIdentity{}, fmt.Errorf("%w: open process %d: %v", ErrProcessIdentityUnavailable, pid, err)
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return ProcessIdentity{}, ErrProcessNotFound
		}
		return ProcessIdentity{}, fmt.Errorf("%w: process times %d: %v", ErrProcessIdentityUnavailable, pid, err)
	}
	var exitCode uint32
	if err := windows.GetExitCodeProcess(handle, &exitCode); err == nil && exitCode != 259 { // STILL_ACTIVE
		return ProcessIdentity{}, ErrProcessNotFound
	}
	buffer := make([]uint16, 4096)
	size := uint32(len(buffer))
	if err := windows.QueryFullProcessImageName(handle, 0, &buffer[0], &size); err != nil {
		if errors.Is(err, windows.ERROR_GEN_FAILURE) || errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return ProcessIdentity{}, ErrProcessNotFound
		}
		return ProcessIdentity{}, fmt.Errorf("%w: process image %d: %v", ErrProcessIdentityUnavailable, pid, err)
	}
	return ProcessIdentity{
		PID:        pid,
		StartedAt:  time.Unix(0, creation.Nanoseconds()),
		Executable: windows.UTF16ToString(buffer[:size]),
	}, nil
}
