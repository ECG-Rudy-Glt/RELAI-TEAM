package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(Event{Type: RepoDeclared, Repo: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Append(Event{Type: ItemCreated, Repo: "a", ItemID: "a/x"}); err != nil {
		t.Fatal(err)
	}
	l.Close()

	// Reopen: sequence numbers continue, nothing is lost.
	l, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	e, err := l.Append(Event{Type: ItemClaimed, ItemID: "a/x", Identity: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	if e.Seq != 3 {
		t.Errorf("seq after reopen = %d, want 3", e.Seq)
	}

	events, err := Replay(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{RepoDeclared, ItemCreated, ItemClaimed}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}
	for i, w := range want {
		if events[i].Type != w || events[i].Seq != int64(i+1) {
			t.Errorf("event %d = %+v, want type %s seq %d", i, events[i], w, i+1)
		}
	}
}

func TestReplayEmptyAndMissing(t *testing.T) {
	dir := t.TempDir()
	if ev, err := Replay(filepath.Join(dir, "missing.jsonl")); err != nil || len(ev) != 0 {
		t.Fatalf("missing log: %v, %v", ev, err)
	}
	empty := filepath.Join(dir, "empty.jsonl")
	_ = os.WriteFile(empty, nil, 0o644)
	if ev, err := Replay(empty); err != nil || len(ev) != 0 {
		t.Fatalf("empty log: %v, %v", ev, err)
	}
}

func TestReplayClaimReleasePairsInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, _ := Open(path)
	for _, e := range []Event{
		{Type: ItemClaimed, ItemID: "x", Identity: "alice"},
		{Type: ItemReleased, ItemID: "x", Identity: "alice"},
		{Type: ItemClaimed, ItemID: "x", Identity: "bob"},
	} {
		if _, err := l.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	l.Close()
	events, err := Replay(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[2].Identity != "bob" || events[1].Type != ItemReleased {
		t.Fatalf("unexpected replay: %+v", events)
	}
}

func TestTornLastLineIsDroppedOnOpen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, _ := Open(path)
	_, _ = l.Append(Event{Type: RepoDeclared, Repo: "a"})
	l.Close()
	// Simulate a crash mid-write.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(`{"seq":2,"type":"item_cre`)
	f.Close()

	l, err := Open(path)
	if err != nil {
		t.Fatalf("open after torn write: %v", err)
	}
	e, err := l.Append(Event{Type: ItemCreated, ItemID: "a/x"})
	l.Close()
	if err != nil || e.Seq != 2 {
		t.Fatalf("append after torn write: %+v, %v", e, err)
	}
	events, err := Replay(path)
	if err != nil || len(events) != 2 {
		t.Fatalf("replay after recovery: %+v, %v", events, err)
	}
}

func TestCorruptMiddleLineIsAnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	_ = os.WriteFile(path, []byte("{\"seq\":1,\"type\":\"repo_declared\"}\nnot json\n{\"seq\":3,\"type\":\"repo_declared\"}\n"), 0o644)
	if _, err := Replay(path); err == nil {
		t.Fatal("expected an error for a corrupt line in the middle of the log")
	}
}
