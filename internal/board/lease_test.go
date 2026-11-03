package board

import (
	"encoding/json"
	"errors"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
)

// clock is a settable fake clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{t: time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC)} }

func leasedBoard(t *testing.T, path string, ck *clock, lease time.Duration) (*Board, *store.Log, string) {
	t.Helper()
	b, l := openBoard(t, path)
	b.SetClock(ck.now)
	b.SetLease(lease)
	if err := b.MergeAggregated(refs("svc"), []aggregate.Item{item("svc", "a", "A task")}); err != nil {
		t.Fatal(err)
	}
	return b, l, "svc/bd/a"
}

func TestLease_LapsedClaimFreesTheItem(t *testing.T) {
	ck := newClock()
	b, l, id := leasedBoard(t, filepath.Join(t.TempDir(), "e.jsonl"), ck, 8*time.Hour)
	defer l.Close()
	if _, err := b.Claim(id, "agent-1"); err != nil {
		t.Fatal(err)
	}
	ck.add(7*time.Hour + 59*time.Minute)
	if it, _ := b.Item(id); it.Holder != "agent-1" || it.ExpiresAt.IsZero() {
		t.Fatalf("one minute before the lease ends: %+v", it)
	}
	var held *HeldError
	if _, err := b.Claim(id, "agent-2"); !errors.As(err, &held) || held.Holder != "agent-1" {
		t.Errorf("claim before lapse = %v, want held by agent-1", err)
	}
	ck.add(2 * time.Minute)
	if it, _ := b.Item(id); it.Holder != "" || !it.ExpiresAt.IsZero() || !it.ClaimedAt.IsZero() {
		t.Errorf("after the lease: %+v, want unheld", it)
	}
	for _, it := range b.Items() {
		if it.Holder != "" {
			t.Errorf("Items() still shows a lapsed holder: %+v", it)
		}
	}
	if it, err := b.Claim(id, "agent-2"); err != nil || it.Holder != "agent-2" {
		t.Errorf("claim after lapse = %+v %v", it, err)
	}
}

func TestLease_RenewingByClaimingAgain(t *testing.T) {
	ck := newClock()
	b, l, id := leasedBoard(t, filepath.Join(t.TempDir(), "e.jsonl"), ck, 8*time.Hour)
	defer l.Close()
	b.Claim(id, "agent-1")
	ck.add(7 * time.Hour)
	it, err := b.Claim(id, "agent-1")
	if err != nil || it.Holder != "agent-1" {
		t.Fatalf("renew: %+v %v", it, err)
	}
	if want := ck.t.Add(8 * time.Hour); !it.ExpiresAt.Equal(want) {
		t.Errorf("expires %v, want %v (lease restarts at renewal)", it.ExpiresAt, want)
	}
	ck.add(7 * time.Hour) // 14h after the first claim, but 7h after the renewal
	if it, _ := b.Item(id); it.Holder != "agent-1" {
		t.Errorf("renewed claim lapsed too early: %+v", it)
	}
}

func TestLease_LateReleaseByLapsedHolderIsRefused(t *testing.T) {
	ck := newClock()
	b, l, id := leasedBoard(t, filepath.Join(t.TempDir(), "e.jsonl"), ck, time.Hour)
	defer l.Close()
	b.Claim(id, "agent-1")
	ck.add(2 * time.Hour)
	var nh *NotHolderError
	if _, err := b.Release(id, "agent-1"); !errors.As(err, &nh) || nh.Holder != "" {
		t.Errorf("late release of an unheld item = %v, want 'not held'", err)
	}
	b.Claim(id, "agent-2")
	if _, err := b.Release(id, "agent-1"); !errors.As(err, &nh) || nh.Holder != "agent-2" {
		t.Errorf("late release after takeover = %v, want held by agent-2", err)
	}
	if it, _ := b.Item(id); it.Holder != "agent-2" {
		t.Errorf("takeover disturbed: %+v", it)
	}
}

func TestLease_SurvivesRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.jsonl")
	ck := newClock()
	b, l, id := leasedBoard(t, path, ck, 8*time.Hour)
	b.Claim(id, "agent-1")
	ck.add(5 * time.Hour)
	b.Claim("svc/bd/a", "agent-1") // renewed at +5h
	l.Close()

	ck.add(6 * time.Hour) // +11h: past the first claim's lease, within the renewal's
	b2, l2 := openBoard(t, path)
	defer l2.Close()
	b2.SetClock(ck.now)
	b2.SetLease(8 * time.Hour)
	if it, _ := b2.Item(id); it.Holder != "agent-1" {
		t.Errorf("after restart a live (renewed) claim was lost: %+v", it)
	}
	ck.add(3 * time.Hour) // +14h: renewal ended at +13h
	if it, _ := b2.Item(id); it.Holder != "" {
		t.Errorf("after restart a lapsed claim came back: %+v", it)
	}
}

func TestLease_ZeroMeansNeverLapses(t *testing.T) {
	ck := newClock()
	b, l, id := leasedBoard(t, filepath.Join(t.TempDir(), "e.jsonl"), ck, 0)
	defer l.Close()
	b.Claim(id, "agent-1")
	ck.add(10 * 365 * 24 * time.Hour)
	it, _ := b.Item(id)
	if it.Holder != "agent-1" || !it.ExpiresAt.IsZero() {
		t.Errorf("with no lease: %+v", it)
	}
}

func TestForceRelease_FreesAndRecordsWho(t *testing.T) {
	path := filepath.Join(t.TempDir(), "e.jsonl")
	ck := newClock()
	b, l, id := leasedBoard(t, path, ck, 8*time.Hour)
	b.Claim(id, "agent-1")
	if it, err := b.ForceRelease(id, "alice"); err != nil || it.Holder != "" {
		t.Fatalf("force release: %+v %v", it, err)
	}
	var nh *NotHolderError
	if _, err := b.ForceRelease(id, "alice"); !errors.As(err, &nh) {
		t.Errorf("forcing an unheld item = %v, want 'not held'", err)
	}
	l.Close()

	raw, _ := os.ReadFile(path)
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e store.Event
		_ = json.Unmarshal([]byte(line), &e)
		if e.Type == store.ItemReleased {
			found = true
			if e.Identity != "agent-1" || e.By != "alice" {
				t.Errorf("released event = identity %q by %q, want agent-1 by alice", e.Identity, e.By)
			}
		}
	}
	if !found {
		t.Error("no release event in the log")
	}
	b2, l2 := openBoard(t, path)
	defer l2.Close()
	if it, _ := b2.Item(id); it.Holder != "" {
		t.Errorf("after replay the forced release was lost: %+v", it)
	}
}
