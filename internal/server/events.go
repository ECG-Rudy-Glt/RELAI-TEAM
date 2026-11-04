package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"
)

// hub wakes every waiting subscriber when the board changes. Closing a channel wakes all of its
// waiters at once and a fresh one is installed under the same lock, so no change can slip between
// a subscriber waking up and waiting again: at worst two changes merge into one wake-up, which is
// fine because a notification only says "re-fetch" and carries no data.
type hub struct {
	mu   sync.Mutex
	ch   chan struct{}
	ver  uint64
	done chan struct{} // closed on shutdown: every stream returns
	once sync.Once
}

func newHub() *hub { return &hub{ch: make(chan struct{}), done: make(chan struct{})} }

func (h *hub) close() { h.once.Do(func() { close(h.done) }) }

// wait returns the channel that will be closed by the next change, and the current version.
func (h *hub) wait() (<-chan struct{}, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ch, h.ver
}

func (h *hub) notify() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ver++
	close(h.ch)
	h.ch = make(chan struct{})
}

// digest summarises everything a client can see change: items (title, status, holder) and which
// repos are readable. It deliberately leaves out timestamps, so an idle refresh digests the same.
func (s *Server) digest() string {
	h := sha256.New()
	for _, it := range s.visibleItems() {
		fmt.Fprintf(h, "I|%s|%s|%s|%s|%s\n", it.ID, it.Title, it.Status, it.Holder, it.Priority)
	}
	s.mu.RLock()
	var repos []string
	for src, st := range s.status {
		repos = append(repos, fmt.Sprintf("R|%s|%v", src, st.Readable))
	}
	s.mu.RUnlock()
	sort.Strings(repos)
	for _, r := range repos {
		fmt.Fprintln(h, r)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// checkChanged notifies subscribers if what clients can see differs from the last time it ran.
// Call it after anything that may change the board: a refresh, a claim, a release.
func (s *Server) checkChanged() {
	d := s.digest()
	s.dmu.Lock()
	changed := d != s.lastDigest
	s.lastDigest = d
	s.dmu.Unlock()
	if changed {
		s.hub.notify()
	}
}

// events streams "change" notifications (Server-Sent Events). The stream is long-lived, so the
// server-wide write deadline is lifted for it alone.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no") // reverse proxies must not buffer the stream
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 3000\n: connected\n\n")
	fl.Flush()

	keepalive := time.NewTicker(20 * time.Second)
	defer keepalive.Stop()
	ch, _ := s.hub.wait()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-s.hub.done:
			return
		case <-ch:
			var ver uint64
			ch, ver = s.hub.wait()
			if _, err := fmt.Fprintf(w, "event: change\ndata: {\"version\":%d}\n\n", ver); err != nil {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
		}
		fl.Flush()
	}
}
