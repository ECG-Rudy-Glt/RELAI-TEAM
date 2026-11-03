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

// Board is safe for concurrent use.
type Board struct {
	mu    sync.Mutex
	log   Appender
	repos map[string]bool
	items map[string]*Item
}

// New builds a board from past events, then records new ones through log.
func New(log Appender, past []store.Event) *Board {
	b := &Board{log: log, repos: map[string]bool{}, items: map[string]*Item{}}
	for _, e := range past {
		b.apply(e)
	}
	return b
}

func (b *Board) apply(e store.Event) {
	switch e.Type {
	case store.RepoDeclared:
		b.repos[e.Repo] = true
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
		out = append(out, *it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RepoItems returns the items of one repo, and whether the board knows that repo at all.
func (b *Board) RepoItems(repo string) ([]Item, bool) {
	b.mu.Lock()
	known := b.repos[repo]
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
	return *it, true
}

// Claim gives the item to who, if nobody holds it. Under contention exactly one caller wins; the
// others get a *HeldError naming the winner. Claiming an item you already hold succeeds again.
func (b *Board) Claim(id, who string) (Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	switch it.Holder {
	case who:
		return *it, nil
	case "":
	default:
		return Item{}, &HeldError{Holder: it.Holder}
	}
	if err := b.record(store.Event{Type: store.ItemClaimed, ItemID: id, Repo: it.Repo, Identity: who}); err != nil {
		return Item{}, err
	}
	return *it, nil
}

// Release frees the item. Only its holder can release it.
func (b *Board) Release(id, who string) (Item, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	it, ok := b.items[id]
	if !ok {
		return Item{}, ErrNotFound
	}
	if it.Holder != who {
		return Item{}, &NotHolderError{Holder: it.Holder}
	}
	if err := b.record(store.Event{Type: store.ItemReleased, ItemID: id, Repo: it.Repo, Identity: who}); err != nil {
		return Item{}, err
	}
	return *it, nil
}

// MergeAggregated reconciles a fresh aggregation into the board. readRepos are the repos that were
// read successfully this round: items of those repos that no longer exist are removed (the work
// is gone, e.g. a closed Beads task), while repos that could not be read keep their items — a
// temporarily unreachable repo must not wipe the board. Holders of items that still exist are
// never touched.
func (b *Board) MergeAggregated(readRepos []string, items []aggregate.Item) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	read := map[string]bool{}
	for _, r := range readRepos {
		read[r] = true
		if !b.repos[r] {
			if err := b.record(store.Event{Type: store.RepoDeclared, Repo: r}); err != nil {
				return err
			}
		}
	}
	seen := map[string]bool{}
	for _, in := range items {
		seen[in.ID] = true
		ev := store.Event{ItemID: in.ID, Repo: in.Repo, Kind: in.Kind, Title: in.Title, Status: in.Status}
		cur, ok := b.items[in.ID]
		switch {
		case !ok:
			ev.Type = store.ItemCreated
		case cur.Title != in.Title || cur.Status != in.Status || cur.Kind != in.Kind:
			ev.Type = store.ItemUpdated
		default:
			continue
		}
		if err := b.record(ev); err != nil {
			return err
		}
	}
	var gone []string
	for id, it := range b.items {
		if read[it.Repo] && !seen[id] {
			gone = append(gone, id)
		}
	}
	sort.Strings(gone)
	for _, id := range gone {
		if err := b.record(store.Event{Type: store.ItemRemoved, ItemID: id, Repo: b.items[id].Repo}); err != nil {
			return err
		}
	}
	return nil
}
