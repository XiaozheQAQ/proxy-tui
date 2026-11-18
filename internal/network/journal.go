package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
)

// journalPhase tracks how far a network change got before the process stopped.
type journalPhase string

const (
	phasePrepared   journalPhase = "prepared"
	phaseRestoring  journalPhase = "restoring"
	phaseRolledBack journalPhase = "rolled-back"
)

// transactionJournal is written before touching the system and removed after a
// successful commit, so an interrupted change can always be reverted.
type transactionJournal struct {
	Phase     journalPhase `json:"phase"`
	Adapter   string       `json:"adapter"`
	StartedAt time.Time    `json:"started_at"`
	Desired   Desired      `json:"desired"`
	Snapshot  Snapshot     `json:"snapshot"`
	Error     string       `json:"error,omitempty"`
}

// Backup keeps the settings that were in place before ProxyTUI enabled the
// desktop proxy, plus the configuration it installed.
type Backup struct {
	At       time.Time  `json:"at"`
	Adapter  string     `json:"adapter"`
	Desired  Desired    `json:"desired"`
	Snapshot Snapshot   `json:"snapshot"`
	Original ProxyState `json:"original"`
}

func (m *Manager) journalPath() string {
	return filepath.Join(m.paths.RuntimeDir(), "network-journal.json")
}

func (m *Manager) backupPath() string {
	return filepath.Join(m.paths.RuntimeDir(), "network-backup.json")
}

func (m *Manager) writeJournal(entry transactionJournal) error {
	if err := os.MkdirAll(m.paths.RuntimeDir(), 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	return storage.WriteJSONAtomic(m.journalPath(), entry, 0o600)
}

func (m *Manager) readJournal() (transactionJournal, bool) {
	entry := transactionJournal{}
	if err := storage.ReadJSON(m.journalPath(), &entry); err != nil {
		return transactionJournal{}, false
	}
	if entry.Adapter == "" {
		return transactionJournal{}, false
	}
	return entry, true
}

func (m *Manager) removeJournal() error {
	err := os.Remove(m.journalPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (m *Manager) writeBackup(backup Backup) error {
	if err := os.MkdirAll(m.paths.RuntimeDir(), 0o700); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}
	return storage.WriteJSONAtomic(m.backupPath(), backup, 0o600)
}

func (m *Manager) readBackup() (*Backup, error) {
	backup := Backup{}
	err := storage.ReadJSON(m.backupPath(), &backup)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read network backup: %w", err)
	}
	if backup.Adapter == "" || len(backup.Snapshot.Data) == 0 {
		return nil, nil
	}
	return &backup, nil
}

func (m *Manager) removeBackup() error {
	err := os.Remove(m.backupPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ActiveBackup reports whether a committed backup exists.
func (m *Manager) ActiveBackup() (*Backup, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	backup, err := m.readBackup()
	if err != nil || backup == nil {
		return nil, false
	}
	return backup, true
}

var _ = json.Marshal
