//go:build windows

package core

import (
	"fmt"
	"os"
)

// terminate ends the process. Windows has no SIGTERM for console children, so
// the core is killed directly.
func terminate(process *os.Process) error {
	if process == nil {
		return nil
	}
	if err := process.Kill(); err != nil {
		return fmt.Errorf("kill process %d: %w", process.Pid, err)
	}
	return nil
}
