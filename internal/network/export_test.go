package network

import (
	"time"

	"github.com/proxytui/proxy-tui/internal/storage"
)

// TransactionJournal mirrors transactionJournal for tests.
type TransactionJournal = transactionJournal

// WriteJournalForTest persists a journal entry for a test.
func WriteJournalForTest(paths storage.Paths, entry TransactionJournal) error {
	manager := NewManager(paths, nil)
	return manager.writeJournal(entry)
}

// JournalForTest reads the journal, if any.
func JournalForTest(paths storage.Paths) *TransactionJournal {
	manager := NewManager(paths, nil)
	entry, ok := manager.readJournal()
	if !ok {
		return nil
	}
	return &entry
}

var _ = time.Now
