// Package server exposes the board over HTTP: token-authenticated reads of repos and work items,
// and claim/release of items. Item IDs contain slashes, so clients URL-encode them in paths
// (`/items/billing-api%2Fbd%2Fb-1/claim`).
package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
	"log"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/board"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/discover"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/mirror"
)

// RepoStatus is what the server knows about one declared repo.
type RepoStatus struct {
	Source   string    `json:"source"`
	ID       string    `json:"id,omitempty"`
	Summary  string    `json:"summary,omitempty"`
	Readable bool      `json:"readable"`
	Error    string    `json:"error,omitempty"`
	Notes    []string  `json:"notes,omitempty"`
	LastRead time.Time `json:"last_read,omitempty"`
	Items    int       `json:"items"`
	// What the repo's relai.yaml says it provides, consumes and works with.
	aggregate.Manifest
}

// authState is what a reload can change: the members' token hashes and who may force a release.
type authState struct {
	tokens map[[32]byte]string
	admins map[string]bool
}

// limits are the request limiters, rebuilt when the limits in the config change.
type limits struct {
	authFail   *limiter
	perMember  *limiter
	trustProxy bool
}

func newAuth(cfg Config) (*authState, error) {
	tokens, err := cfg.tokens()
	if err != nil {
		return nil, err
	}
	admins := map[string]bool{}
	for _, m := range cfg.Members {
		if m.Admin {
			admins[m.Identity] = true
		}
	}
	return &authState{tokens: tokens, admins: admins}, nil
}

// Reload applies the parts of a new config that can change while the server runs: the members and their
// tokens (so a token can be rotated or revoked without a restart and without losing a claim) and the
// request limits. Everything else (repos, listen address, TLS...) needs a restart; the caller says so.
func (s *Server) Reload(cfg Config) error {
	a, err := newAuth(cfg)
	if err != nil {
		return err
	}
	s.auth.Store(a)
	s.limits.Store(&limits{
		authFail:   newLimiter(cfg.AuthFailureLimit(), s.clock),
		perMember:  newLimiter(cfg.RequestLimit(), s.clock),
		trustProxy: cfg.TrustProxy,
	})
	return nil
}

// Server serves one team's board.
type Server struct {
	board    *board.Board
	agg      *aggregate.Aggregator
	declared []string
	mirrors  []*mirror.Mirror // read-only issue mirrors, each shown as a repo named by its source
	mc       mentionCache
	scan     []DiscoverConfig          // folders scanned for repositories at each refresh
	found    []string                  // repositories found there at the last refresh (guarded by mu)
	auth     atomic.Pointer[authState] // who may get in; replaced by Reload without a restart
	limits   atomic.Pointer[limits]
	clock    func() time.Time // for tests

	mu     sync.RWMutex
	status map[string]RepoStatus // by source

	hub        *hub
	dmu        sync.Mutex
	lastDigest string

	refreshMu sync.Mutex  // one refresh at a time (the initial read can overlap the first tick)
	loading   atomic.Bool // true from SetLoading until the first Refresh has finished
}

// SetLoading makes the data endpoints answer 503 "loading" until the first Refresh completes, so
// the process can start listening (and answer /healthz) before a slow first read of many repos.
func (s *Server) SetLoading() { s.loading.Store(true) }

// CloseStreams ends every open /events stream, so a graceful shutdown does not wait for clients
// that would never hang up by themselves.
func (s *Server) CloseStreams() { s.hub.close() }

// New wires a server. agg may use a fake command runner in tests.
func New(cfg Config, b *board.Board, agg *aggregate.Aggregator) (*Server, error) {
	mirrors, err := cfg.Mirrors()
	if err != nil {
		return nil, err
	}
	b.SetLease(cfg.ClaimLeaseDuration())
	srv := &Server{
		board:    b,
		agg:      agg,
		declared: append([]string(nil), cfg.Repos...),
		mirrors:  mirrors,
		scan:     append([]DiscoverConfig(nil), cfg.Discover...),
		status:   map[string]RepoStatus{},
		hub:      newHub(),
	}
	if err := srv.Reload(cfg); err != nil {
		return nil, err
	}
	return srv, nil
}

// Refresh re-reads every declared repo and merges the result into the board, so a task's status
// change shows up without anyone rebuilding an index. A repo that can't be read this round keeps
// its items and is reported as unreadable.
func (s *Server) Refresh() error {
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	err := s.refresh()
	s.checkChanged()
	s.loading.Store(false)
	return err
}

func (s *Server) refresh() error {
	read, skipped := s.agg.All(s.sourcesToRead())
	for _, m := range s.mirrors {
		if r, err := m.Read(); err != nil {
			skipped = append(skipped, aggregate.Skip{Source: m.Src.Name(), Reason: err.Error()})
		} else {
			read = append(read, r)
		}
	}
	// Two repos with the same relai.yaml id would share item ids, and a claim on one would show
	// on the other. The first declared keeps the id; the others are reported as unreadable, with
	// the reason, until one of them changes its id.
	repos := read[:0:0]
	firstWithID := map[string]string{}
	for _, r := range read {
		if other, dup := firstWithID[r.ID]; dup && r.ID != "" {
			skipped = append(skipped, aggregate.Skip{Source: r.Source, Reason: fmt.Sprintf("id %q is already used by %s: give each repo its own id in relai.yaml", r.ID, other)})
			continue
		}
		firstWithID[r.ID] = r.Source
		repos = append(repos, r)
	}
	refs := make([]board.RepoRef, 0, len(repos))
	for _, r := range repos {
		refs = append(refs, board.RepoRef{ID: r.ID, Source: r.Source})
	}
	if err := s.board.MergeAggregated(refs, aggregate.Items(repos)); err != nil {
		return err
	}

	idBySource := map[string]string{}
	for id, src := range s.board.Repos() {
		idBySource[src] = id
	}
	now := time.Now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range repos {
		s.status[r.Source] = RepoStatus{
			Source: r.Source, ID: r.ID, Summary: r.Summary, Readable: true,
			Notes: r.Notes, LastRead: now, Manifest: r.Manifest,
		}
	}
	for _, sk := range skipped {
		prev := s.status[sk.Source]
		prev.Source, prev.Readable, prev.Error, prev.Notes = sk.Source, false, sk.Reason, nil
		if prev.ID == "" {
			prev.ID = idBySource[sk.Source]
		}
		s.status[sk.Source] = prev
	}
	return nil
}

// sourcesToRead is the declared repos plus the repositories found in the discover folders. The folders are
// scanned again every time: a project cloned there appears on the next refresh, and one removed becomes
// unreadable (its items and claims are kept, as for any repo that cannot be read).
func (s *Server) sourcesToRead() []string {
	out := append([]string(nil), s.declared...)
	if len(s.scan) == 0 {
		return out
	}
	have := map[string]bool{}
	for _, d := range out {
		if real, err := filepath.EvalSymlinks(d); err == nil {
			have[real] = true
		}
		have[d] = true
	}
	var found []string
	for _, d := range s.scan {
		depth := d.Depth
		if depth == 0 {
			depth = 1
		}
		paths, err := discover.Find(d.Folder, depth)
		if err != nil {
			log.Printf("discover: %v", err)
			continue
		}
		for _, p := range paths {
			real, rerr := filepath.EvalSymlinks(p)
			if rerr != nil {
				real = p
			}
			if have[p] || have[real] {
				continue
			}
			have[p], have[real] = true, true
			found = append(found, p)
		}
	}
	s.mu.Lock()
	s.found = found
	s.mu.Unlock()
	return append(out, found...)
}

// sources is every source the server shows: the declared repos, those found by discovery, then the issue mirrors.
func (s *Server) sources() []string {
	out := append([]string(nil), s.declared...)
	s.mu.RLock()
	out = append(out, s.found...)
	s.mu.RUnlock()
	for _, m := range s.mirrors {
		out = append(out, m.Src.Name())
	}
	return out
}

// visibleRepos maps the IDs of repos that are both declared in the config and known to the board
// (read now or in an earlier run) to their source.
func (s *Server) visibleRepos() map[string]string {
	declared := map[string]bool{}
	for _, src := range s.sources() {
		declared[src] = true
	}
	out := map[string]string{}
	for id, src := range s.board.Repos() {
		if declared[src] {
			out[id] = src
		}
	}
	return out
}

// Handler returns the HTTP API. Everything but /healthz requires a team member's token.
func (s *Server) Handler() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /me", s.me)
	api.HandleFunc("GET /repos", s.repos)
	api.HandleFunc("GET /repos/{repo}/items", s.repoItems)
	api.HandleFunc("GET /items", s.items)
	api.HandleFunc("GET /orphans", s.orphans)
	api.HandleFunc("GET /items/{id}", s.item)
	api.HandleFunc("POST /items/{id}/claim", s.claim)
	api.HandleFunc("POST /items/{id}/release", s.release)
	api.HandleFunc("GET /events", s.events)
	api.HandleFunc("GET /changes", s.changes)
	api.HandleFunc("GET /links", s.links)
	api.HandleFunc("GET /find", s.find)

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		// Always 200: the process is alive. "ready" says whether the first read is finished.
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "ready": !s.loading.Load()})
	})
	root.HandleFunc("GET /board", s.boardPage)
	root.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/board", http.StatusSeeOther)
	})
	root.Handle("/", s.authenticate(api))
	return root
}

type ctxKey struct{}

// authenticate rejects any request without a recognized `Authorization: Bearer <token>` before it
// reaches a handler — no board data is ever returned to an unknown caller.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lim := s.limits.Load()
		addr := clientAddr(r, lim.trustProxy)
		// A client that has used up its wrong guesses is turned away BEFORE its token is looked at: otherwise
		// the limit would not stop a guess from being tried.
		if ok, retry := lim.authFail.peek(addr); !ok {
			tooMany(w, retry, "too many wrong tokens from this address")
			return
		}
		h := r.Header.Get("Authorization")
		// The scheme name is case-insensitive (RFC 7235); the token itself is compared exactly.
		token, ok := "", false
		if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
			token, ok = h[7:], true
		}
		if !ok || token == "" {
			lim.authFail.take(addr)
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		who, ok := s.auth.Load().tokens[sha256.Sum256([]byte(token))]
		if !ok {
			lim.authFail.take(addr)
			writeError(w, http.StatusUnauthorized, "token does not belong to a member of this team")
			return
		}
		if ok, retry := lim.perMember.take(who); !ok {
			tooMany(w, retry, "too many requests from "+who)
			return
		}
		if s.loading.Load() {
			w.Header().Set("Retry-After", "5")
			writeError(w, http.StatusServiceUnavailable, "loading: the first read of the repos is not finished, retry in a few seconds")
			return
		}
		next.ServeHTTP(w, r.WithContext(withIdentity(r.Context(), who)))
	})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"identity": identity(r)})
}

func (s *Server) repos(w http.ResponseWriter, _ *http.Request) {
	counts := map[string]int{}
	for _, it := range s.visibleItems() {
		counts[it.Repo]++
	}
	s.mu.RLock()
	srcs := s.sources()
	out := make([]RepoStatus, 0, len(srcs))
	for _, src := range srcs {
		st, ok := s.status[src]
		if !ok {
			st = RepoStatus{Source: src, Error: "not read yet"}
		}
		st.Items = counts[st.ID]
		out = append(out, st)
	}
	s.mu.RUnlock()
	writeJSON(w, http.StatusOK, out)
}

// orphans lists the claims held on items of repos the board no longer reads (the repo left the config or
// the folder). They are not on the board any more, but the journal still holds them: show who and what,
// without titles, so they can be released or forgotten on purpose.
func (s *Server) orphans(w http.ResponseWriter, _ *http.Request) {
	visible := s.visibleRepos()
	type orphan struct {
		ID     string `json:"id"`
		Repo   string `json:"repo"`
		Holder string `json:"holder"`
	}
	out := []orphan{}
	for _, it := range s.board.Items() {
		if _, ok := visible[it.Repo]; !ok && it.Holder != "" {
			out = append(out, orphan{it.ID, it.Repo, it.Holder})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"orphaned_claims": out})
}

func (s *Server) repoItems(w http.ResponseWriter, r *http.Request) {
	repo := r.PathValue("repo")
	if _, ok := s.visibleRepos()[repo]; !ok {
		writeError(w, http.StatusNotFound, "repo not tracked: "+repo)
		return
	}
	items, _ := s.board.RepoItems(repo)
	writeJSON(w, http.StatusOK, nonNil(items))
}

// items lists every visible item; ?repo= and ?holder= filter (holder=me is the caller).
func (s *Server) items(w http.ResponseWriter, r *http.Request) {
	repo, holder := r.URL.Query().Get("repo"), r.URL.Query().Get("holder")
	if holder == "me" {
		holder = identity(r)
	}
	var out []board.Item
	for _, it := range s.visibleItems() {
		if (repo == "" || it.Repo == repo) && (holder == "" || it.Holder == holder) {
			out = append(out, it)
		}
	}
	writeJSON(w, http.StatusOK, nonNil(out))
}

// change is one thing that happened on the board, as clients see it.
type change struct {
	Seq      int64     `json:"seq"`
	Time     time.Time `json:"time"`
	Type     string    `json:"type"` // item_created, item_updated, item_removed, item_claimed, item_released
	ItemID   string    `json:"item_id"`
	Repo     string    `json:"repo,omitempty"`
	Identity string    `json:"identity,omitempty"`
	By       string    `json:"by,omitempty"`
	Title    string    `json:"title,omitempty"`
	Status   string    `json:"status,omitempty"`
}

// changes answers "what happened since sequence number N?" so a client that already knows the
// board does not have to list it again: GET /changes?since=N gives the board's current seq, the
// changes after N (only for repos this server shows), and complete=false when N is older than the
// history kept. Without since it gives just the current seq and the latest few changes. With net=1 it keeps
// only the last change of each item.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	since, tail := int64(0), false
	if v := q.Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "since must be a sequence number, 0 or more")
			return
		}
		since = n
	} else {
		tail = true
	}
	events, seq, complete := s.board.Changes(since)
	visible := s.visibleRepos()
	out := []change{}
	for _, e := range events {
		if e.Type == store.RepoDeclared || e.ItemID == "" {
			continue
		}
		if _, ok := visible[e.Repo]; !ok {
			continue
		}
		out = append(out, change{Seq: e.Seq, Time: e.Time, Type: e.Type, ItemID: e.ItemID, Repo: e.Repo,
			Identity: e.Identity, By: e.By, Title: e.Title, Status: e.Status})
	}
	// net=1: only the last thing that happened to each item. Claiming and releasing the same item ten times
	// is one line ("it is free now"), which is what a client coming back needs; the full sequence is
	// for audit, not for catching up.
	net := q.Get("net") == "1"
	if net {
		last := map[string]int{}
		for i, c := range out {
			last[c.ItemID] = i
		}
		var merged []change
		for i, c := range out {
			if last[c.ItemID] == i {
				merged = append(merged, c)
			}
		}
		out = merged
	}
	limit := 200
	if tail {
		limit = 10
	}
	truncated := false
	if len(out) > limit {
		out, truncated = out[len(out)-limit:], true
	}
	writeJSON(w, http.StatusOK, map[string]any{"seq": seq, "complete": complete && !(truncated && !tail), "changes": out})
}

func (s *Server) item(w http.ResponseWriter, r *http.Request) {
	it, ok := s.visibleItem(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such item")
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (s *Server) claim(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.visibleItem(id); !ok {
		writeError(w, http.StatusNotFound, "no such item")
		return
	}
	it, err := s.board.Claim(id, identity(r))
	var held *board.HeldError
	switch {
	case err == nil:
		s.checkChanged()
		writeJSON(w, http.StatusOK, it)
	case errors.As(err, &held):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "holder": held.Holder})
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such item")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) release(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.visibleItem(id); !ok {
		writeError(w, http.StatusNotFound, "no such item")
		return
	}
	who := identity(r)
	var it board.Item
	var err error
	if r.URL.Query().Get("force") == "true" {
		if !s.auth.Load().admins[who] {
			writeError(w, http.StatusForbidden, "forcing a release needs an administrator")
			return
		}
		it, err = s.board.ForceRelease(id, who)
	} else {
		it, err = s.board.Release(id, who)
	}
	var nh *board.NotHolderError
	switch {
	case err == nil:
		s.checkChanged()
		writeJSON(w, http.StatusOK, it)
	case errors.As(err, &nh):
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error(), "holder": nh.Holder})
	case errors.Is(err, board.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such item")
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) visibleItems() []board.Item {
	visible := s.visibleRepos()
	var out []board.Item
	for _, it := range s.board.Items() {
		if _, ok := visible[it.Repo]; ok {
			out = append(out, it)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Server) visibleItem(id string) (board.Item, bool) {
	it, ok := s.board.Item(id)
	if !ok {
		return board.Item{}, false
	}
	if _, ok := s.visibleRepos()[it.Repo]; !ok {
		return board.Item{}, false
	}
	return it, true
}

func nonNil(items []board.Item) []board.Item {
	if items == nil {
		return []board.Item{}
	}
	return items
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
