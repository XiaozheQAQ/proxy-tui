//go:build linux

package core

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Linux /proc starttime is expressed in USER_HZ units (100 on all supported
// Linux ABI targets). btime gives the boot wall-clock second.
const linuxClockTicks = 100

func readProcessIdentity(pid int) (ProcessIdentity, error) {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ProcessIdentity{}, ErrProcessNotFound
		}
		return ProcessIdentity{}, fmt.Errorf("%w: read /proc/%d/stat: %v", ErrProcessIdentityUnavailable, pid, err)
	}
	text := string(stat)
	closeIndex := strings.LastIndex(text, ")")
	if closeIndex < 0 || closeIndex+2 >= len(text) {
		return ProcessIdentity{}, fmt.Errorf("%w: malformed /proc/%d/stat", ErrProcessIdentityUnavailable, pid)
	}
	fields := strings.Fields(text[closeIndex+2:])
	if len(fields) < 20 {
		return ProcessIdentity{}, fmt.Errorf("%w: short /proc/%d/stat", ErrProcessIdentityUnavailable, pid)
	}
	if fields[0] == "Z" {
		return ProcessIdentity{}, ErrProcessNotFound
	}
	startTicks, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil {
		return ProcessIdentity{}, fmt.Errorf("%w: start time for %d", ErrProcessIdentityUnavailable, pid)
	}
	bootSeconds, err := linuxBootTime()
	if err != nil {
		return ProcessIdentity{}, err
	}
	executable, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ProcessIdentity{}, fmt.Errorf("%w: executable for %d: %v", ErrProcessIdentityUnavailable, pid, err)
	}
	started := time.Unix(bootSeconds, 0).Add(time.Duration(startTicks) * time.Second / linuxClockTicks)
	return ProcessIdentity{PID: pid, StartedAt: started, Executable: executable}, nil
}

func linuxBootTime() (int64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, fmt.Errorf("%w: read /proc/stat: %v", ErrProcessIdentityUnavailable, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "btime ") {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(line, "btime ")), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%w: parse btime", ErrProcessIdentityUnavailable)
		}
		return value, nil
	}
	return 0, fmt.Errorf("%w: btime missing", ErrProcessIdentityUnavailable)
}
