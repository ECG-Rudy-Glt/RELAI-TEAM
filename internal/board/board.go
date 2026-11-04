// Package board holds the live board: work items, each with at most one holder. Every change is
// appended to the event log before it takes effect, so the board can always be rebuilt by
// replaying the log.
package board

import (
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
)

// Item is a work item as the board shows it.
type Item struct {
	aggregate.Item
	Holder    string    `json:"holder,omitempty"`
	ClaimedAt time.Time `json:"claimed_at,omitempty"`
	// ExpiresAt is when the claim lapses unless renewed; zero when unheld or leases are off.
	// Computed when read, never stored.
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// ErrNotFound is returned for an item the board does not have.
var ErrNotFound = errors.New("no such item")

// HeldError is returned when an item is already held by someone else.
type HeldError struct{ Holder string }

func (e *HeldError) Error() string { return "already held by " + e.Holder }

// NotHolderError is returned when someone other than the holder tries to release an item.
type NotHolderError struct{ Holder string }

func (e *NotHolderError) Error() string {
	if e.Holder == "" {
		return "item is not held"
	}
	return "held by " + e.Holder + ", not by you"
}

// Appender persists an event; *store.Log implements it.
type Appender interface {
	Append(store.Event) (store.Event, error)
}

// RepoRef identifies a repo read in an aggregation round.
type RepoRef struct {
	ID     string
	Source string
}

// Board is safe for concurrent use.
type Board struct {
	mu    sync.Mutex
	log   Appender
	repos map[string]string // repo ID -> source it was last read from
	items map[string]*Item

	lease time.Duration // 0: claims never lapse
	now   func() time.Time
}

// SetLease makes claims lapse after d unless renewed (0 turns lapsing off). It applies to claims
// already in the log too, because a claim's age is derived from the time of its event.
func (b *Board) SetLease(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lease = d
}

// SetClock replaces the clock; for tests.
func (b *Board) SetClock(now func() time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.now = now
}

func (b *Board) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// holder is who holds the item right now: nobody once its lease has lapsed. Callers hold b.mu.
func (b *Board) holder(it *Item) string {
	if it.Holder != "" && b.lease > 0 && !b.clock().Before(it.ClaimedAt.Add(b.lease)) {
		return ""
	}
	return it.Holder
}

// view is the item as clients see it. Callers hold b.mu.
func (b *Board) view(it *Item) Item {
	v := *it
	v.Holder = b.holder(it)
	if v.Holder == "" {
		v.ClaimedAt = time.Time{}
	} else if b.lease > 0 {
		v.ExpiresAt = it.ClaimedAt.Add(b.lease)
	}
	return v
}

// New builds a board from past events, then records new ones through log.
func New(log Appender, past []store.Event) *Board {
	b := &Board{log: log, repos: map[string]string{}, items: map[string]*Item{}}
	for _, e := range past {
		b.apply(e)
	}
	return b
}

func (b *Board) apply(e store.Event) {
	switch e.Type {
	case store.RepoDeclared:
		b.repos[e.Repo] = e.Source
	case store.ItemCreated, store.ItemUpdated:
		it, ok := b.items[e.ItemID]
		if !ok {
			it = &Item{}
			b.items[e.ItemID] = it
		}
		it.ID, it.Repo, it.Kind, it.Title, it.Status = e.ItemID, e.Repo, e.Kind, e.Title, e.Status
	case store.ItemRemoved:
		delete(b.items, e.ItemID)
	case store.ItemClaimed:
		if it, ok := b.items[e.ItemID]; ok {
			it.Holder, it.ClaimedAt = e.Identity, e.Time
		}
	case store.ItemReleased:
		if it, ok := b.items[e.ItemID]; ok {
			it.Holder, it.ClaimedAt = "", time.Time{}
		}
	}
}

// recordAll appends the events with a single flush when the log supports it, then applies them in
// order. Nothing is applied if the append fails. Callers hold b.mu.
func (b *Board) recordAll(events []store.Event) error {
	if len(events) == 0 {
		return nil
	}
	bl, ok := b.log.(interface {
		AppendBatch([]store.Event) ([]store.Event, error)
	})
	if !ok {
		for _, e := range events {
			if err := b.record(e); err != nil {
				return err
			}
		}
		return nil
	}
	stored, err := bl.AppendBatch(events)
	if err != nil {
		return fmt.Errorf("persist %d events: %w", len(events), err)
	}
	for _, e := range stored {
		b.apply(e)
	}
	return nil
}

// record appends e to the log, then applies it. Callers hold b.mu.
func (b *Board) record(e store.Event) error {
	stored, err := b.log.Append(e)
	if err != nil {
		return fmt.Errorf("persist %s: %w", e.Type, err)
	}
	b.apply(stored)
	return nil
}

// Items returns every item, sorted by ID.
func (b *Board) Items() []Item {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]Item, 0, len(b.items))
	for _, it := range b.items {
		out = append(out, b.view(it))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Repos returns every repo the board has ever read, by ID, with the source it was read from.
func (b *Board) Repos() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]string, len(b.repos))
	for id, src := range b.repos {
		out[id] = src
	}
	return out
}

// RepoItems returns the items of one repo, and whether the board knows that repo at all.
func (b *Board) RepoItems(repo string) ([]Item, bool) {
	b.mu.Lock()
	_, known := b.repos[repo]
	b.mu.Unlock()
	if !known {
		return nil, false
	}
	var out []Item
	for _, it := range b.Items() {
		if it.Repo == repo {
			out = append(out, it)
		}
	}
	return out, true
}

// Item returns one item.
func (b *Board) Item(id string) (Item, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[id]
	if !ok {
		return Item{}, false
	}
	return b.view(it), true
}

// Claim gives the item to who if nobody holds it, or its claim has lapsed. Under contention exactly
// one caller wins; the others get a *HeldError naming the winner. Claiming an item you already hold
// renews your claim.
func (b *Board) Claim(id, who string) (Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	if h := b.holder(it); h != "" && h != who {
		return Item{}, &HeldError{Holder: h}
	}
	if err := b.record(store.Event{Type: store.ItemClaimed, ItemID: id, Repo: it.Repo, Identity: who, Time: b.clock().UTC()}); err != nil {
		return Item{}, err
	}
	return b.view(it), nil
}

// Release frees the item. Only its holder can release it.
func (b *Board) Release(id, who string) (Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	if h := b.holder(it); h != who {
		return Item{}, &NotHolderError{Holder: h}
	}
	if err := b.record(store.Event{Type: store.ItemReleased, ItemID: id, Repo: it.Repo, Identity: who}); err != nil {
		return Item{}, err
	}
	return b.view(it), nil
}

// ForceRelease frees an item whoever holds it, on behalf of the administrator by. The event keeps
// the former holder as its identity and the administrator in By, so the history says who freed what.
func (b *Board) ForceRelease(id, by string) (Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	h := b.holder(it)
	if h == "" {
		return Item{}, &NotHolderError{}
	}
	if err := b.record(store.Event{Type: store.ItemReleased, ItemID: id, Repo: it.Repo, Identity: h, By: by}); err != nil {
		return Item{}, err
	}
	return b.view(it), nil
}

// MergeAggregated reconciles a fresh aggregation into the board. readRepos are the repos that were
// read successfully this round: items of those repos that no longer exist are removed (the work
// is gone, e.g. a closed Beads task), while repos that could not be read keep their items — a
// temporarily unreachable repo must not wipe the board. Holders of items that still exist are
// never touched.
func (b *Board) MergeAggregated(readRepos []RepoRef, items []aggregate.Item) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Work out every event first, then write them with one flush: the first read of hundreds of
	// repos is thousands of events, and a flush each cost ~100 MiB of disk writes.
	var pending []store.Event
	read := map[string]bool{}
	for _, r := range readRepos {
		read[r.ID] = true
		if src, ok := b.repos[r.ID]; !ok || src != r.Source {
			pending = append(pending, store.Event{Type: store.RepoDeclared, Repo: r.ID, Source: r.Source})
		}
	}
	planned := map[string]aggregate.Item{} // items created or updated by pending events, as they will look
	seen := map[string]bool{}
	for _, in := range items {
		seen[in.ID] = true
		ev := store.Event{ItemID: in.ID, Repo: in.Repo, Kind: in.Kind, Title: in.Title, Status: in.Status}
		var cur aggregate.Item
		p, wasPlanned := planned[in.ID]
		ok := wasPlanned
		if wasPlanned {
			cur = p
		} else if bi, exists := b.items[in.ID]; exists {
			cur, ok = bi.Item, true
		}
		switch {
		case !ok:
			ev.Type = store.ItemCreated
		case cur.Title != in.Title || cur.Status != in.Status || cur.Kind != in.Kind:
			ev.Type = store.ItemUpdated
		default:
			continue
		}
		planned[in.ID] = aggregate.Item{ID: in.ID, Repo: in.Repo, Kind: in.Kind, Title: in.Title, Status: in.Status}
		pending = append(pending, ev)
	}
	var gone []string
	for id, it := range b.items {
		if read[it.Repo] && !seen[id] {
			gone = append(gone, id)
		}
	}
	sort.Strings(gone)
	for _, id := range gone {
		pending = append(pending, store.Event{Type: store.ItemRemoved, ItemID: id, Repo: b.items[id].Repo})
	}
	return b.recordAll(pending)
}
