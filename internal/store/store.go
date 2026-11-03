// Package store persists board state as an append-only JSON-lines event log. Every state change is
// one line; the board is rebuilt by replaying the log from the start. No external database.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// Event types.
const (
	RepoDeclared = "repo_declared"
	ItemCreated  = "item_created"
	ItemUpdated  = "item_updated"
	ItemRemoved  = "item_removed"
	ItemClaimed  = "item_claimed"
	ItemReleased = "item_released"
)

// Event is one line of the log.
type Event struct {
	Seq      int64     `json:"seq"`
	Time     time.Time `json:"time"`
	Type     string    `json:"type"`
	Repo     string    `json:"repo,omitempty"`
	Source   string    `json:"source,omitempty"` // repo_declared: the path or URL it is read from
	ItemID   string    `json:"item_id,omitempty"`
	Identity string    `json:"identity,omitempty"`
	Title    string    `json:"title,omitempty"`
	Kind     string    `json:"kind,omitempty"`
	Status   string    `json:"status,omitempty"`
}

// Log appends events to a file, fsyncing after each write so an acknowledged event survives a
// crash.
type Log struct {
	mu   sync.Mutex
	f    *os.File
	next int64
}

// Open opens (or creates) the log at path, positioned to append after its last event.
func Open(path string) (*Log, error) {
	if err := truncateTornTail(path); err != nil {
		return nil, err
	}
	events, err := Replay(path)
	if err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	next := int64(1)
	if n := len(events); n > 0 {
		next = events[n-1].Seq + 1
	}
	return &Log{f: f, next: next}, nil
}

// Append writes e as the next event, assigning its sequence number and time, and returns it as
// stored.
func (l *Log) Append(e Event) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e.Seq = l.next
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return Event{}, err
	}
	if _, err := l.f.Write(append(b, '\n')); err != nil {
		return Event{}, err
	}
	if err := l.f.Sync(); err != nil {
		return Event{}, err
	}
	l.next++
	return e, nil
}

// truncateTornTail drops a final line without a trailing newline — a write cut short by a crash —
// so the next append starts on a clean line.
func truncateTornTail(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if len(b) == 0 || b[len(b)-1] == '\n' {
		return nil
	}
	keep := 0
	for i := len(b) - 1; i >= 0; i-- {
		if b[i] == '\n' {
			keep = i + 1
			break
		}
	}
	return os.Truncate(path, int64(keep))
}

// Close closes the underlying file.
func (l *Log) Close() error { return l.f.Close() }

// Replay reads every event in the log at path, in order. A missing file is an empty log.
func Replay(path string) ([]Event, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	line := 0
	var bad error
	for sc.Scan() {
		line++
		if len(sc.Bytes()) == 0 {
			continue
		}
		if bad != nil {
			return nil, bad
		}
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			// Tolerated only if it is the last line: a write torn by a crash.
			bad = fmt.Errorf("%s:%d: %w", path, line, err)
			continue
		}
		out = append(out, e)
	}
	return out, sc.Err()
}
