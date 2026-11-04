package store

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
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

func TestReplayWarnsAboutUnknownTypesAndNonIncreasingSeq(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	body := "{\"seq\":1,\"type\":\"item_claimed\",\"item_id\":\"a/1\",\"identity\":\"bob\"}\n" +
		"{\"seq\":2,\"type\":\"item_clamed\",\"item_id\":\"a/2\",\"identity\":\"bob\"}\n" +
		"{\"seq\":2,\"type\":\"item_claimed\",\"item_id\":\"a/3\",\"identity\":\"carol\"}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)
	events, err := Replay(path)
	if err != nil || len(events) != 3 {
		t.Fatalf("replay must keep going and keep all lines: %d, %v", len(events), err)
	}
	for _, want := range []string{`unknown event type "item_clamed"`, "events.jsonl:3", "does not follow seq 2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing warning %q in %q", want, out.String())
		}
	}
	out.Reset()
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	if out.Len() != 0 {
		t.Errorf("Open must not repeat the warnings Replay prints: %q", out.String())
	}
}
