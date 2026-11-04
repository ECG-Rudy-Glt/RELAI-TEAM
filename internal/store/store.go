// Package store persists board state as an append-only JSON-lines event log. Every state change is
// one line; the board is rebuilt by replaying the log from the start. No external database.
package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	By       string    `json:"by,omitempty"` // item_released: the administrator who forced it, if any
	Title    string    `json:"title,omitempty"`
	Kind     string    `json:"kind,omitempty"`
	Status   string    `json:"status,omitempty"`
}

// Log appends events to a file, fsyncing after each write so an acknowledged event survives a
// crash.
type Log struct {
	mu   sync.Mutex
	f    *os.File
	lock *os.File // holds the exclusive lock on the journal for as long as the log is open
	next int64
	size int64 // length of the journal up to the end of the last fully written line
	torn bool  // a failed write may have left bytes after size that could not be cut off yet
}

// Open opens (or creates) the log at path, positioned to append after its last event.
func Open(path string) (*Log, error) {
	// Take the lock before touching the file: a second server on the same journal would hand the
	// same item to two people and reuse sequence numbers.
	lock, err := lockJournal(path)
	if err != nil {
		return nil, err
	}
	if err := truncateTornTail(path); err != nil {
		lock.Close()
		return nil, err
	}
	if err := quarantineInvalidLines(path); err != nil {
		lock.Close()
		return nil, err
	}
	events, err := Replay(path)
	if err != nil {
		lock.Close()
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		lock.Close()
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		lock.Close()
		return nil, err
	}
	next := int64(1)
	if n := len(events); n > 0 {
		next = events[n-1].Seq + 1
	}
	return &Log{f: f, lock: lock, next: next, size: st.Size()}, nil
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
	if l.torn {
		// A previous write failed and its leftovers could not be removed; try again before
		// appending anything, so a half line is never followed by a good one.
		if err := l.f.Truncate(l.size); err != nil {
			return Event{}, fmt.Errorf("journal still has a partial line from an earlier failed write: %w", err)
		}
		l.torn = false
	}
	line := append(b, '\n')
	_, err = l.f.Write(line)
	if err == nil {
		err = l.f.Sync()
	}
	if err != nil {
		// The write may have been partial (disk full). Cut the file back to the last good line so
		// the journal stays replayable; the event was not acknowledged, so nothing is lost.
		if terr := l.f.Truncate(l.size); terr != nil {
			l.torn = true
		}
		return Event{}, err
	}
	l.size += int64(len(line))
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

// quarantineInvalidLines moves every line that is not a valid event out of the journal, into
// path+".rejected", so a line damaged by an earlier failure cannot keep the server from starting.
// Each moved line is reported in the log. The journal is rewritten only if something was moved,
// through a temporary file and a rename.
func quarantineInvalidLines(path string) error {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var good, bad bytes.Buffer
	for n, line := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			log.Printf("WARNING: %s:%d is not a valid event (%v); moved to %s.rejected", path, n+1, err, path)
			bad.Write(line)
			bad.WriteByte('\n')
			continue
		}
		good.Write(line)
		good.WriteByte('\n')
	}
	if bad.Len() == 0 {
		return nil
	}
	rf, err := os.OpenFile(path+".rejected", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := rf.Write(bad.Bytes()); err != nil {
		rf.Close()
		return err
	}
	if err := rf.Sync(); err != nil {
		rf.Close()
		return err
	}
	if err := rf.Close(); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, good.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Close closes the underlying file.
func (l *Log) Close() error {
	err := l.f.Close()
	if l.lock != nil {
		if lerr := l.lock.Close(); err == nil {
			err = lerr
		}
	}
	return err
}

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
