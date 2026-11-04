package board

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
)

func openBoard(t *testing.T, path string) (*Board, *store.Log) {
	t.Helper()
	past, err := store.Replay(path)
	if err != nil {
		t.Fatal(err)
	}
	l, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return New(l, past), l
}

func refs(ids ...string) []RepoRef {
	var out []RepoRef
	for _, id := range ids {
		out = append(out, RepoRef{ID: id, Source: "/repos/" + id})
	}
	return out
}

func item(repo, id, title string) aggregate.Item {
	return aggregate.Item{ID: repo + "/bd/" + id, Repo: repo, Kind: aggregate.KindTask, Title: title, Status: "open"}
}

func TestReplayRestoresHolder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	b, l := openBoard(t, path)
	if err := b.MergeAggregated(refs("r"), []aggregate.Item{item("r", "1", "Task one")}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Claim("r/bd/1", "alice"); err != nil {
		t.Fatal(err)
	}
	l.Close()

	// Restart: a new board built from the same log.
	b2, l2 := openBoard(t, path)
	defer l2.Close()
	it, ok := b2.Item("r/bd/1")
	if !ok || it.Holder != "alice" || it.Title != "Task one" {
		t.Fatalf("after restart: %+v (found=%v)", it, ok)
	}
}

func TestConcurrentClaimsExactlyOneWins(t *testing.T) {
	b, l := openBoard(t, filepath.Join(t.TempDir(), "events.jsonl"))
	defer l.Close()
	_ = b.MergeAggregated(refs("r"), []aggregate.Item{item("r", "1", "Contended")})

	const n = 16
	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, results[i] = b.Claim("r/bd/1", fmt.Sprintf("agent-%d", i))
		}(i)
	}
	close(start)
	wg.Wait()

	winner, _ := b.Item("r/bd/1")
	wins := 0
	for _, err := range results {
		var held *HeldError
		switch {
		case err == nil:
			wins++
		case errors.As(err, &held):
			if held.Holder != winner.Holder {
				t.Errorf("loser told holder is %q, actual holder %q", held.Holder, winner.Holder)
			}
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 {
		t.Fatalf("%d claims succeeded, want exactly 1", wins)
	}
}

func TestReleaseRules(t *testing.T) {
	b, l := openBoard(t, filepath.Join(t.TempDir(), "events.jsonl"))
	defer l.Close()
	_ = b.MergeAggregated(refs("r"), []aggregate.Item{item("r", "1", "T")})

	var nh *NotHolderError
	if _, err := b.Release("r/bd/1", "alice"); !errors.As(err, &nh) {
		t.Errorf("releasing an unheld item: %v", err)
	}
	if _, err := b.Claim("r/bd/1", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Claim("r/bd/1", "alice"); err != nil {
		t.Errorf("re-claiming your own item should succeed: %v", err)
	}
	if _, err := b.Release("r/bd/1", "bob"); !errors.As(err, &nh) || nh.Holder != "alice" {
		t.Errorf("bob releasing alice's item: %v", err)
	}
	if _, err := b.Release("r/bd/1", "alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Claim("r/bd/1", "bob"); err != nil {
		t.Errorf("released item should be claimable by someone else: %v", err)
	}
	if _, err := b.Claim("r/bd/nope", "bob"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown item: %v", err)
	}
}

func TestMergeAggregated(t *testing.T) {
	b, l := openBoard(t, filepath.Join(t.TempDir(), "events.jsonl"))
	defer l.Close()
	_ = b.MergeAggregated(refs("a", "b"), []aggregate.Item{
		item("a", "1", "Keep me"),
		item("a", "2", "Will close"),
		item("b", "1", "In repo b"),
	})
	if _, err := b.Claim("a/bd/1", "alice"); err != nil {
		t.Fatal(err)
	}

	// Next round: a/1 renamed (still held), a/2 closed, a/3 new; repo b unreadable this round.
	if err := b.MergeAggregated(refs("a"), []aggregate.Item{
		item("a", "1", "Keep me (renamed)"),
		item("a", "3", "Brand new"),
	}); err != nil {
		t.Fatal(err)
	}

	if it, ok := b.Item("a/bd/1"); !ok || it.Holder != "alice" || it.Title != "Keep me (renamed)" {
		t.Errorf("held item after merge: %+v", it)
	}
	if _, ok := b.Item("a/bd/2"); ok {
		t.Error("closed task should be removed")
	}
	if _, ok := b.Item("a/bd/3"); !ok {
		t.Error("new item should appear")
	}
	if _, ok := b.Item("b/bd/1"); !ok {
		t.Error("items of a repo that could not be read this round must be kept")
	}
	if items, known := b.RepoItems("a"); !known || len(items) != 2 {
		t.Errorf("RepoItems(a) = %v, %v", items, known)
	}
	if _, known := b.RepoItems("zzz"); known {
		t.Error("unknown repo reported as known")
	}
}

type failingLog struct{}

func (failingLog) Append(store.Event) (store.Event, error) {
	return store.Event{}, errors.New("disk full")
}

func TestClaimNotAppliedWhenPersistFails(t *testing.T) {
	b := New(failingLog{}, []store.Event{
		{Seq: 1, Type: store.ItemCreated, ItemID: "r/bd/1", Repo: "r", Title: "T"},
	})
	if _, err := b.Claim("r/bd/1", "alice"); err == nil {
		t.Fatal("claim must fail when it cannot be persisted")
	}
	if it, _ := b.Item("r/bd/1"); it.Holder != "" {
		t.Errorf("unpersisted claim leaked into the board: %+v", it)
	}
}

// flushCounter is a log that records how many separate writes the board asked for.
type flushCounter struct {
	flushes, events int
	next            int64
}

func (f *flushCounter) Append(e store.Event) (store.Event, error) {
	f.flushes++
	f.events++
	f.next++
	e.Seq = f.next
	return e, nil
}

func (f *flushCounter) AppendBatch(es []store.Event) ([]store.Event, error) {
	f.flushes++
	out := make([]store.Event, len(es))
	for i, e := range es {
		f.events++
		f.next++
		e.Seq = f.next
		out[i] = e
	}
	return out, nil
}

func TestFirstReadOfManyItemsIsOneFlush(t *testing.T) {
	log := &flushCounter{}
	b := New(log, nil)
	var items []aggregate.Item
	for i := 0; i < 50; i++ {
		items = append(items, aggregate.Item{ID: fmt.Sprintf("r/%d", i), Repo: "r", Title: "t", Status: "open"})
	}
	// the same id twice in one read: the second must be seen as an update only if it differs
	items = append(items, aggregate.Item{ID: "r/0", Repo: "r", Title: "t", Status: "open"})
	if err := b.MergeAggregated([]RepoRef{{ID: "r", Source: "/r"}}, items); err != nil {
		t.Fatal(err)
	}
	if log.flushes != 1 || log.events != 51 {
		t.Errorf("flushes=%d events=%d, want 1 flush for 1 repo + 50 items", log.flushes, log.events)
	}
	if len(b.Items()) != 50 {
		t.Errorf("board has %d items, want 50", len(b.Items()))
	}
	if err := b.MergeAggregated([]RepoRef{{ID: "r", Source: "/r"}}, items); err != nil || log.flushes != 1 {
		t.Errorf("an unchanged read must write nothing: flushes=%d err=%v", log.flushes, err)
	}
	// a removal and a change are written together
	if err := b.MergeAggregated([]RepoRef{{ID: "r", Source: "/r"}}, append(items[1:50:50], aggregate.Item{ID: "r/1", Repo: "r", Title: "changed", Status: "open"})); err != nil {
		t.Fatal(err)
	}
	if log.flushes != 2 {
		t.Errorf("changes should be one more flush, got %d in total", log.flushes)
	}
}
