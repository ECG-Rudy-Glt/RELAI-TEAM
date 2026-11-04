//go:build unix

package store

import (
	"bytes"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// limitFileSize makes writes fail once a file is larger than n bytes, the way a
// full disk does: a write that crosses the limit is partial.
func limitFileSize(t *testing.T, n int64) (restore func()) {
	t.Helper()
	signal.Ignore(syscall.SIGXFSZ)
	var old syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &old); err != nil {
		t.Skip("RLIMIT_FSIZE not available")
	}
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: uint64(n), Max: old.Max}); err != nil {
		t.Skip("cannot set RLIMIT_FSIZE")
	}
	return func() { _ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &old) }
}

func TestPartialWriteDoesNotPoisonTheJournal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := l.Append(Event{Type: ItemClaimed, ItemID: "a/1", Identity: "bob"}); err != nil {
			t.Fatal(err)
		}
	}
	st, _ := os.Stat(path)
	restore := limitFileSize(t, st.Size()+20) // room for a fragment of the next line only
	_, err = l.Append(Event{Type: ItemReleased, ItemID: "a/1", Identity: "bob"})
	restore()
	if err == nil {
		t.Fatal("append should fail while the disk is full")
	}
	// Space is back: the same process keeps going, numbering continues without a gap.
	e, err := l.Append(Event{Type: ItemClaimed, ItemID: "a/2", Identity: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Seq != 4 {
		t.Errorf("seq after failed write = %d, want 4", e.Seq)
	}
	l.Close()

	l, err = Open(path)
	if err != nil {
		t.Fatalf("restart after a full disk: %v", err)
	}
	l.Close()
	events, err := Replay(path)
	if err != nil || len(events) != 4 {
		t.Fatalf("replay: %d events, %v; want 4", len(events), err)
	}
	if _, err := os.Stat(path + ".rejected"); err == nil {
		t.Error("nothing should have needed quarantine: the partial line was cut off")
	}
}

func TestInvalidLinesAreQuarantinedNotFatal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	// A journal damaged by the old bug: a half line followed by good ones, plus an invalid last
	// line that does end with a newline.
	damaged := "{\"seq\":1,\"type\":\"repo_declared\",\"repo\":\"a\"}\n" +
		"{\"seq\":2,\"time\":\"202{\"seq\":2,\"type\":\"item_claimed\",\"item_id\":\"a/1\",\"identity\":\"bob\"}\n" +
		"{\"seq\":3,\"type\":\"item_released\",\"item_id\":\"a/1\",\"identity\":\"bob\"}\n" +
		"garbage\n"
	if err := os.WriteFile(path, []byte(damaged), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	log.SetOutput(&out)
	defer log.SetOutput(os.Stderr)

	l, err := Open(path)
	if err != nil {
		t.Fatalf("open a damaged journal: %v", err)
	}
	l.Close()
	if !strings.Contains(out.String(), "events.jsonl:2") || !strings.Contains(out.String(), "events.jsonl:4") {
		t.Errorf("each skipped line must be reported, got log: %q", out.String())
	}
	rej, err := os.ReadFile(path + ".rejected")
	if err != nil || !strings.Contains(string(rej), "garbage") || !strings.Contains(string(rej), `"time":"202{`) {
		t.Errorf("rejected lines not kept: %q, %v", rej, err)
	}
	events, err := Replay(path)
	if err != nil || len(events) != 2 {
		t.Fatalf("replay after quarantine: %d events, %v; want 2", len(events), err)
	}
}

func TestAppendBatchIsAllOrNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	l, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.AppendBatch([]Event{{Type: RepoDeclared, Repo: "a"}, {Type: ItemCreated, ItemID: "a/1", Repo: "a"}}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	restore := limitFileSize(t, st.Size()+150) // room for part of the next batch only
	var batch []Event
	for i := 0; i < 10; i++ {
		batch = append(batch, Event{Type: ItemCreated, ItemID: "a/x", Repo: "a"})
	}
	_, err = l.AppendBatch(batch)
	restore()
	if err == nil {
		t.Fatal("the batch should fail on a full disk")
	}
	e, err := l.Append(Event{Type: ItemClaimed, ItemID: "a/1", Identity: "bob"})
	if err != nil || e.Seq != 3 {
		t.Fatalf("append after a failed batch: %+v %v (want seq 3: the failed batch used no numbers)", e, err)
	}
	l.Close()
	events, err := Replay(path)
	if err != nil || len(events) != 3 {
		t.Fatalf("replay: %d events, %v; want 3 and none of the failed batch", len(events), err)
	}
}
