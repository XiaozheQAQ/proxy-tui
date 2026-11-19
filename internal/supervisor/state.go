package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
)

// State is persisted so clients can discover a running supervisor.
type State struct {
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Endpoint  string    `json:"endpoint"`
	Version   string    `json:"version"`
	Protocol  int       `json:"protocol"`
}

// StatePath returns the supervisor state file.
func StatePath(paths storage.Paths) string {
	return filepath.Join(paths.RuntimeDir(), "supervisor-state.json")
}

// LoadState reads the persisted supervisor state, if any.
func LoadState(paths storage.Paths) (*State, error) {
	state := State{}
	if err := storage.ReadJSON(StatePath(paths), &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	if state.Endpoint == "" {
		return nil, nil
	}
	return &state, nil
}

// SaveState persists the supervisor state atomically.
func SaveState(paths storage.Paths, state State) error {
	if err := os.MkdirAll(paths.RuntimeDir(), 0o700); err != nil {
		return err
	}
	return storage.WriteJSONAtomic(StatePath(paths), state, 0o600)
}

// RemoveState removes the state file.
func RemoveState(paths storage.Paths) {
	_ = os.Remove(StatePath(paths))
}
