//go:build !windows

package core

import (
	"fmt"
	"os"
	"syscall"
)

// terminate asks the core to shut down gracefully.
func terminate(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("signal process %d: %w", process.Pid, err)
	}
	return nil
}
