package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
	"github.com/proxytui/proxy-tui/internal/subscription/model"
)

// RuntimeState is persisted when a core starts so a later process can detect
// and stop a core that outlived the process which launched it.
type RuntimeState struct {
	Core       model.CoreKind `json:"core"`
	PID        int            `json:"pid"`
	APIAddr    string         `json:"api_addr"`
	Secret     string         `json:"secret"`
	ProxyPort  int            `json:"proxy_port"`
	ConfigPath string         `json:"config_path"`
	Version    string         `json:"version"`
	StartedAt  time.Time      `json:"started_at"`
}

// StatePath is where a core's runtime state is stored.
func StatePath(paths storage.Paths, kind model.CoreKind) string {
	return filepath.Join(paths.RuntimeDir(), string(kind)+"-state.json")
}

// WriteRuntimeState persists the state atomically with owner-only permissions.
func WriteRuntimeState(paths storage.Paths, state RuntimeState) error {
	if err := os.MkdirAll(paths.RuntimeDir(), 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	return storage.WriteJSONAtomic(StatePath(paths, state.Core), state, 0o600)
}

// ReadRuntimeState loads the persisted state, if any.
func ReadRuntimeState(paths storage.Paths, kind model.CoreKind) (RuntimeState, bool) {
	state := RuntimeState{}
	if err := storage.ReadJSON(StatePath(paths, kind), &state); err != nil {
		return RuntimeState{}, false
	}
	if state.PID <= 0 || state.APIAddr == "" {
		return RuntimeState{}, false
	}
	return state, true
}

// RemoveRuntimeState deletes the persisted state.
func RemoveRuntimeState(paths storage.Paths, kind model.CoreKind) error {
	err := os.Remove(StatePath(paths, kind))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// KillPID terminates a process by pid, treating an already dead process as
// success.
func KillPID(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("find process %d: %w", pid, err)
	}
	if err := process.Kill(); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return fmt.Errorf("kill process %d: %w", pid, err)
	}
	return nil
}
