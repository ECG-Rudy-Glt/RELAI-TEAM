// Package server exposes the board over HTTP: token-authenticated reads of repos and work items,
// and claim/release of items. Item IDs contain slashes, so clients URL-encode them in paths
// (`/items/billing-api%2Fbd%2Fb-1/claim`).
package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/board"
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
}

// Server serves one team's board.
type Server struct {
	board    *board.Board
	agg      *aggregate.Aggregator
	declared []string
	tokens   map[[32]byte]string

	mu     sync.RWMutex
	status map[string]RepoStatus // by source
}

// New wires a server. agg may use a fake command runner in tests.
func New(cfg Config, b *board.Board, agg *aggregate.Aggregator) (*Server, error) {
	tokens, err := cfg.tokens()
	if err != nil {
		return nil, err
	}
	return &Server{
		board:    b,
		agg:      agg,
		declared: append([]string(nil), cfg.Repos...),
		tokens:   tokens,
		status:   map[string]RepoStatus{},
	}, nil
}

// Refresh re-reads every declared repo and merges the result into the board, so a task's status
// change shows up without anyone rebuilding an index. A repo that can't be read this round keeps
// its items and is reported as unreadable.
func (s *Server) Refresh() error {
	repos, skipped := s.agg.All(s.declared)
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
			Notes: r.Notes, LastRead: now,
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

// visibleRepos maps the IDs of repos that are both declared in the config and known to the board
// (read now or in an earlier run) to their source.
func (s *Server) visibleRepos() map[string]string {
	declared := map[string]bool{}
	for _, src := range s.declared {
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
	api.HandleFunc("GET /items/{id}", s.item)
	api.HandleFunc("POST /items/{id}/claim", s.claim)
	api.HandleFunc("POST /items/{id}/release", s.release)

	root := http.NewServeMux()
	root.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	root.Handle("/", s.authenticate(api))
	return root
}

type ctxKey struct{}

// authenticate rejects any request without a recognized `Authorization: Bearer <token>` before it
// reaches a handler — no board data is ever returned to an unknown caller.
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		token, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || token == "" {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		who, ok := s.tokens[sha256.Sum256([]byte(token))]
		if !ok {
			writeError(w, http.StatusUnauthorized, "token does not belong to a member of this team")
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
	out := make([]RepoStatus, 0, len(s.declared))
	for _, src := range s.declared {
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
	it, err := s.board.Release(id, identity(r))
	var nh *board.NotHolderError
	switch {
	case err == nil:
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
