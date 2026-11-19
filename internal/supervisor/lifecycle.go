package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
)

// LifecycleStatus distinguishes a user stop from a crash. A crash leaves the
// RUNNING record behind; a graceful stop writes INTENTIONALLY_STOPPED.
type LifecycleStatus string

const (
	LifecycleRunning LifecycleStatus = "RUNNING"
	LifecycleStopped LifecycleStatus = "INTENTIONALLY_STOPPED"
)

// LifecycleRecord is persisted next to supervisor-state.json.
type LifecycleRecord struct {
	Status    LifecycleStatus `json:"status"`
	PID       int             `json:"pid"`
	StartedAt time.Time       `json:"started_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// LifecyclePath returns the persisted lifecycle record path.
func LifecyclePath(paths storage.Paths) string {
	return filepath.Join(paths.RuntimeDir(), "supervisor-lifecycle.json")
}

// LoadLifecycle reads the lifecycle record, returning nil when absent.
func LoadLifecycle(paths storage.Paths) (*LifecycleRecord, error) {
	record := LifecycleRecord{}
	if err := storage.ReadJSON(LifecyclePath(paths), &record); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// SaveLifecycle writes the record atomically.
func SaveLifecycle(paths storage.Paths, record LifecycleRecord) error {
	if err := os.MkdirAll(paths.RuntimeDir(), 0o700); err != nil {
		return err
	}
	record.UpdatedAt = time.Now()
	return storage.WriteJSONAtomic(LifecyclePath(paths), record, 0o600)
}
