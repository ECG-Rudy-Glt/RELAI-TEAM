//go:build unix

package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestStandbyWaitsForTheActiveServerThenTakesOverWithItsData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	active, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := active.Append(Event{Type: ItemClaimed, ItemID: "x/1", Identity: "bob"}); err != nil {
		t.Fatal(err)
	}
	got := make(chan *Log, 1)
	go func() {
		l, err := OpenWait(path)
		if err != nil {
			t.Error(err)
		}
		got <- l
	}()
	select {
	case <-got:
		t.Fatal("the standby took over while the active server was still running")
	case <-time.After(300 * time.Millisecond):
	}
	active.Close() // the active server stops (a crash releases the lock the same way)
	select {
	case l := <-got:
		e, err := l.Append(Event{Type: ItemReleased, ItemID: "x/1", Identity: "bob"})
		if err != nil || e.Seq != 2 {
			t.Errorf("the standby must continue the journal: %+v %v", e, err)
		}
		l.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("the standby did not take over after the active server stopped")
	}
}
