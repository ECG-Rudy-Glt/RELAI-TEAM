package server

import (
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/discover"
)

// repoView is a repository as the links view shows it.
type repoView struct {
	ID       string   `json:"id"`
	Source   string   `json:"source"`
	Summary  string   `json:"summary,omitempty"`
	Readable bool     `json:"readable"`
	Provides []string `json:"provides,omitempty"`
	Consumes []string `json:"consumes,omitempty"`
	Links    []string `json:"links,omitempty"`     // declared by this repo's relai.yaml
	LinkedBy []string `json:"linked_by,omitempty"` // repos that declare a link to this one
}

// edge is a relation between two repos. Kind "declared" comes from a links: entry written by a person;
// kind "capability" from one repo consuming what another provides.
type edge struct {
	From       string `json:"from"`
	To         string `json:"to"`
	Kind       string `json:"kind"`
	Capability string `json:"capability,omitempty"`
}

// candidate is a MENTION of one repo in another, not a verified link. Declared says a person already
// wrote the link down.
type candidate struct {
	discover.Link
	Declared bool `json:"declared"`
}

type linksAnswer struct {
	Repos      []repoView  `json:"repos"`
	Edges      []edge      `json:"edges"`
	Candidates []candidate `json:"candidates,omitempty"`
	Note       string      `json:"note,omitempty"`
}

// mentionCache keeps the result of the (read-only, CPU-bound) search for mentions for a while: it runs
// `git grep` between every pair of repos, which is too much to redo on every request.
type mentionCache struct {
	mu    sync.Mutex
	at    time.Time
	key   string
	links []discover.Link
}

const mentionTTL = 30 * time.Minute

func (s *Server) views() []repoView {
	vis := map[string]bool{}
	for _, src := range s.sources() {
		vis[src] = true
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []repoView
	for src, st := range s.status {
		if !vis[src] || st.ID == "" {
			continue
		}
		out = append(out, repoView{ID: st.ID, Source: src, Summary: st.Summary, Readable: st.Readable,
			Provides: st.Provides, Consumes: st.Consumes, Links: st.Links})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func buildGraph(repos []repoView) ([]repoView, []edge) {
	ids := map[string]int{}
	for i, r := range repos {
		ids[r.ID] = i
	}
	providers := map[string][]string{}
	for _, r := range repos {
		for _, c := range r.Provides {
			providers[strings.ToLower(c)] = append(providers[strings.ToLower(c)], r.ID)
		}
	}
	var edges []edge
	for _, r := range repos {
		for _, to := range r.Links {
			if i, ok := ids[to]; ok {
				edges = append(edges, edge{From: r.ID, To: to, Kind: "declared"})
				repos[i].LinkedBy = append(repos[i].LinkedBy, r.ID)
			}
		}
		for _, c := range r.Consumes {
			for _, p := range providers[strings.ToLower(c)] {
				if p != r.ID {
					edges = append(edges, edge{From: r.ID, To: p, Kind: "capability", Capability: c})
				}
			}
		}
	}
	return repos, edges
}

// links answers GET /links: the repos with what they provide, consume and say they work with, the edges
// that follow from it, and with ?candidates=1 the mentions found in their files (evidence, to be confirmed).
func (s *Server) links(w http.ResponseWriter, r *http.Request) {
	repos, edges := buildGraph(s.views())
	ans := linksAnswer{Repos: repos, Edges: edges}
	if ans.Repos == nil {
		ans.Repos = []repoView{}
	}
	if ans.Edges == nil {
		ans.Edges = []edge{}
	}
	if r.URL.Query().Get("candidates") == "1" {
		declared := map[[2]string]bool{}
		for _, e := range edges {
			if e.Kind == "declared" {
				declared[[2]string{e.From, e.To}] = true
			}
		}
		var local []discover.Repo
		for _, v := range repos {
			if v.Readable && isLocalPath(v.Source) {
				local = append(local, discover.Repo{ID: v.ID, Path: v.Source})
			}
		}
		for _, l := range s.mentions(local) {
			ans.Candidates = append(ans.Candidates, candidate{Link: l, Declared: declared[[2]string{l.From, l.To}]})
		}
		ans.Note = "candidates are mentions found in tracked files, not verified links: strong = the other repo's remote URL or a relative path to it; weak = its name as a word. Confirm before declaring a link in relai.yaml. Only repos on this machine's disk are searched."
	}
	writeJSON(w, http.StatusOK, ans)
}

func isLocalPath(src string) bool {
	return !strings.Contains(src, "://") && !strings.HasPrefix(src, "git@") && !strings.HasPrefix(src, "github:") && !strings.HasPrefix(src, "gitlab:")
}

func (s *Server) mentions(repos []discover.Repo) []discover.Link {
	var ids []string
	for _, r := range repos {
		ids = append(ids, r.ID+"="+r.Path)
	}
	sort.Strings(ids)
	key := strings.Join(ids, "|")
	s.mc.mu.Lock()
	defer s.mc.mu.Unlock()
	if s.mc.key == key && time.Since(s.mc.at) < mentionTTL {
		return s.mc.links
	}
	links := discover.Mentions(repos)
	s.mc.key, s.mc.at, s.mc.links = key, time.Now(), links
	return links
}

// find answers GET /find?capability=x: which repos provide it (case-insensitive) and which consume it, with
// who each works with, so someone about to build it can see that it exists.
func (s *Server) find(w http.ResponseWriter, r *http.Request) {
	want := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("capability")))
	if want == "" {
		writeError(w, http.StatusBadRequest, "capability is required")
		return
	}
	repos, _ := buildGraph(s.views())
	type hit struct {
		repoView
		Role string `json:"role"` // "provides" or "consumes"
	}
	out := []hit{}
	for _, v := range repos {
		for _, c := range v.Provides {
			if strings.ToLower(c) == want {
				out = append(out, hit{v, "provides"})
			}
		}
	}
	for _, v := range repos {
		for _, c := range v.Consumes {
			if strings.ToLower(c) == want {
				out = append(out, hit{v, "consumes"})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"capability": r.URL.Query().Get("capability"), "repos": out})
}
