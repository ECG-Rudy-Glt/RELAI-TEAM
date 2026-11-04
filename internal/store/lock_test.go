//go:build unix

package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestSecondOpenOnTheSameJournalIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	a, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("second Open: %v, want an 'already in use' error", err)
	}
	// The refused attempt must not have disturbed the first server.
	if _, err := a.Append(Event{Type: ItemClaimed, ItemID: "x/1", Identity: "bob"}); err != nil {
		t.Fatal(err)
	}
	// Once the first one is closed the journal can be opened again.
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	b, err := Open(path)
	if err != nil {
		t.Fatalf("reopen after close: %v", err)
	}
	b.Close()
}
