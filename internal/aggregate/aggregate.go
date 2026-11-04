// Package aggregate reads the Relai repo contract of a declared set of repos — relai.yaml, Beads
// tasks, OpenSpec change tasks — into work items. It talks to Beads and OpenSpec through their own
// CLIs (`bd list --json`, `openspec list --json`) and plain files, exactly as Relai's CLI does; it
// never imports Relai's code, only its data contract.
package aggregate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Item kinds.
const (
	KindTask       = "beads-task"
	KindChangeTask = "openspec-task"
	KindTodo       = "todo-item" // an unchecked box of TODO.md or TASKS.md
)

// Item is one unit of work surfaced on the board.
type Item struct {
	ID     string `json:"id"` // stable across aggregations: <repo>/<kind-prefix>/<native id>
	Repo   string `json:"repo"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// Priority is "P0" (most urgent) to "P4", or empty when the source does not say.
	Priority string `json:"priority,omitempty"`
}

// Repo is what was read from one declared repo.
type Repo struct {
	Source  string   `json:"source"`
	ID      string   `json:"id"`
	Summary string   `json:"summary,omitempty"`
	Items   []Item   `json:"items"`
	Notes   []string `json:"notes,omitempty"` // parts that could not be read, and why
	// Manifest holds what relai.yaml says the repo provides and consumes and who it works with.
	Manifest
}

// Skip is a declared repo that could not be read at all.
type Skip struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// Runner runs a command in dir and returns its standard output. Injectable so tests need neither
// bd nor openspec installed.
type Runner func(dir, name string, args ...string) ([]byte, error)

// ExecRunner runs real commands, with a timeout.
func ExecRunner(dir, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := cmd.Output(); done <- result{out, err} }()
	select {
	case r := <-done:
		return r.out, r.err
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("%s timed out", name)
	}
}

// DefaultWorkers is how many repos are read at once when Workers is not set.
const DefaultWorkers = 8

// DefaultFullRefresh is how long a repo whose fingerprint has not changed is trusted before it is
// read again anyway, as a safety net for a change the fingerprint cannot see.
const DefaultFullRefresh = 6 * time.Hour

// Aggregator reads declared repos.
//
// Reading a repo's Beads state means running `bd`, and every `bd` run rewrites its embedded Dolt
// database (about 2-3 MiB of journal, even for `bd list --readonly`). Polling that on a timer
// wrote ~9 MB/s to disk and overheated an SSD. So a repo is only read when a cheap fingerprint
// (file sizes and times, no process spawned) says its inputs changed, or when FullRefresh has
// passed. See fingerprint.go.
type Aggregator struct {
	Run Runner
	// FullRefresh is the longest an unchanged repo is trusted (0 means DefaultFullRefresh).
	FullRefresh time.Duration
	// Git says how to authenticate to private remotes; RunEnv runs git with that environment.
	Git    GitAuth
	RunEnv EnvRunner
	// NoCache reads every repo on every call. Only for debugging: it is the expensive mode.
	NoCache bool

	cmu   sync.Mutex
	cache map[string]cacheEntry
	reads atomic.Uint64
	hits  atomic.Uint64
	// safety counts reads done only because FullRefresh had passed, not because anything changed.
	safety atomic.Uint64
	// Workers caps how many repos are read concurrently (0 means DefaultWorkers). Each read spawns
	// bd and openspec, so this bounds how many processes a refresh forks at once.
	Workers int
}

// New returns an Aggregator that runs real commands.
func New() *Aggregator { return &Aggregator{Run: ExecRunner, RunEnv: ExecRunnerEnv} }

// All reads every source (local path or clone URL), returning what it could read and what it had
// to skip, in the order the sources were declared. Sources are read concurrently (see Workers), so
// a cycle takes about as long as the slowest repo instead of the sum of all of them. It never
// fails as a whole: one unreadable repo is a Skip, not an error. A source listed twice is read once,
// so no repo is ever read by two workers at the same time.
func (a *Aggregator) All(sources []string) (repos []Repo, skipped []Skip) {
	type result struct {
		repo Repo
		err  error
	}
	var unique []string
	seen := map[string]bool{}
	for _, src := range sources {
		if !seen[src] {
			seen[src] = true
			unique = append(unique, src)
		}
	}
	res := make([]result, len(unique))
	workers := a.Workers
	if workers <= 0 {
		workers = DefaultWorkers
	}
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, src := range unique {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, src string) {
			defer wg.Done()
			defer func() { <-sem }()
			r, err := a.One(src)
			res[i] = result{r, err}
		}(i, src)
	}
	wg.Wait()
	for i, r := range res {
		if r.err != nil {
			skipped = append(skipped, Skip{Source: unique[i], Reason: r.err.Error()})
			continue
		}
		repos = append(repos, r.repo)
	}
	return repos, skipped
}

// Items flattens the items of every repo.
func Items(repos []Repo) []Item {
	var out []Item
	for _, r := range repos {
		out = append(out, r.Items...)
	}
	return out
}

// One reads a single source. A missing path or a directory that is not a git repo is an error;
// a missing relai.yaml, Beads or OpenSpec is not — those parts are just absent, noted in Repo.Notes.
func (a *Aggregator) One(src string) (Repo, error) {
	fp, fpOK := a.fingerprint(src)
	if fpOK && !a.NoCache {
		if r, ok := a.cached(src, fp); ok {
			a.hits.Add(1)
			return r, nil
		}
	}
	r, err := a.read(src)
	if err == nil {
		a.reads.Add(1)
		if fpOK && !a.NoCache {
			a.store(src, fp, r)
		}
	}
	return r, err
}

// Reads is how many repos have actually been read (processes spawned) so far.
func (a *Aggregator) Reads() uint64 { return a.reads.Load() }

// SafetyReads is how many reads were triggered by the FullRefresh safety net rather than by a
// change in the repo's files.
func (a *Aggregator) SafetyReads() uint64 { return a.safety.Load() }

// CacheHits is how many reads were skipped because the repo's fingerprint was unchanged.
func (a *Aggregator) CacheHits() uint64 { return a.hits.Load() }

func (a *Aggregator) read(src string) (Repo, error) {
	dir, cleanup, err := a.fetch(src)
	if err != nil {
		return Repo{}, err
	}
	defer cleanup()
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return Repo{}, errors.New("not found")
	}
	if _, err := a.Run(dir, "git", "rev-parse", "--git-dir"); err != nil {
		return Repo{}, errors.New("not a git repo")
	}

	r := Repo{Source: src, ID: filepath.Base(strings.TrimSuffix(strings.TrimRight(src, "/"), ".git"))}
	if b, err := os.ReadFile(filepath.Join(dir, "relai.yaml")); err == nil {
		if id, summary := parseManifest(b); id != "" {
			r.ID, r.Summary = id, summary
		}
		r.Manifest = parseLists(b)
	} else {
		r.Notes = append(r.Notes, "no relai.yaml")
	}

	if isDir(filepath.Join(dir, ".beads")) {
		items, err := a.beadsItems(dir, r.ID)
		if err != nil {
			r.Notes = append(r.Notes, "beads: "+err.Error())
		}
		r.Items = append(r.Items, items...)
	}
	for _, name := range todoFiles {
		if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			items, note := todoItems(string(b), r.ID, name)
			r.Items = append(r.Items, items...)
			if note != "" {
				r.Notes = append(r.Notes, note)
			}
		}
	}
	if isDir(filepath.Join(dir, "openspec")) {
		items, err := a.openspecItems(dir, r.ID)
		if err != nil {
			r.Notes = append(r.Notes, "openspec: "+err.Error())
		}
		r.Items = append(r.Items, items...)
	}
	return r, nil
}

func (a *Aggregator) beadsItems(dir, repo string) ([]Item, error) {
	out, err := a.Run(dir, "bd", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("bd list: %w", err)
	}
	var tasks []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
		Type   string `json:"issue_type"`
		Prio   *int   `json:"priority"`
	}
	if err := json.Unmarshal(out, &tasks); err != nil {
		return nil, fmt.Errorf("bd list: %w", err)
	}
	var items []Item
	for _, t := range tasks {
		if t.Status == "closed" || t.Type == "epic" {
			continue
		}
		items = append(items, Item{
			ID: repo + "/bd/" + t.ID, Repo: repo, Kind: KindTask, Title: t.Title, Status: t.Status, Priority: prioLabel(t.Prio),
		})
	}
	return items, nil
}

func (a *Aggregator) openspecItems(dir, repo string) ([]Item, error) {
	out, err := a.Run(dir, "openspec", "list", "--json")
	if err != nil {
		return nil, fmt.Errorf("openspec list: %w", err)
	}
	var list struct {
		Changes []struct {
			Name string `json:"name"`
		} `json:"changes"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("openspec list: %w", err)
	}
	var items []Item
	for _, c := range list.Changes {
		b, err := os.ReadFile(filepath.Join(dir, "openspec", "changes", c.Name, "tasks.md"))
		if err != nil {
			continue
		}
		for _, t := range openTasks(string(b)) {
			items = append(items, Item{
				ID:     repo + "/openspec/" + c.Name + "/" + t.num,
				Repo:   repo,
				Kind:   KindChangeTask,
				Title:  c.Name + ": " + t.title,
				Status: "open",
			})
		}
	}
	return items, nil
}

type openTask struct{ num, title string }

var taskNum = regexp.MustCompile(`^(\d+(?:\.\d+)*)\s+(.*)$`)

// openTasks returns the unchecked "- [ ] …" items of a tasks.md. Numbered tasks ("1.2 Do x") keep
// their number as a stable ID; unnumbered ones are numbered by position.
func openTasks(md string) []openTask {
	var out []openTask
	pos := 0
	for _, l := range strings.Split(md, "\n") {
		l = strings.TrimSpace(l)
		var text string
		switch {
		case strings.HasPrefix(l, "- [ ] "):
			text = strings.TrimSpace(l[len("- [ ] "):])
		case strings.HasPrefix(l, "- [x] "), strings.HasPrefix(l, "- [X] "):
			pos++
			continue
		default:
			continue
		}
		pos++
		if m := taskNum.FindStringSubmatch(text); m != nil {
			out = append(out, openTask{num: m[1], title: m[1] + " " + m[2]})
		} else {
			out = append(out, openTask{num: fmt.Sprintf("t%d", pos), title: text})
		}
	}
	return out
}

// parseManifest reads id and summary from relai.yaml — the only two fields the board needs.
func parseManifest(b []byte) (id, summary string) {
	for _, l := range strings.Split(string(b), "\n") {
		if len(l) == 0 || l[0] == ' ' || l[0] == '\t' || l[0] == '#' {
			continue
		}
		k, v, ok := strings.Cut(l, ":")
		if !ok {
			continue
		}
		v = unquote(stripComment(strings.TrimSpace(v)))
		switch strings.TrimSpace(k) {
		case "id":
			id = v
		case "summary":
			summary = v
		}
	}
	return id, summary
}

func stripComment(s string) string {
	var q byte
	for i := 0; i < len(s); i++ {
		switch {
		case q != 0:
			if s[i] == q {
				q = 0
			}
		case s[i] == '"' || s[i] == '\'':
			q = s[i]
		case s[i] == '#':
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

func unquote(s string) string {
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

var cloneURL = regexp.MustCompile(`^(https?|git|ssh|file)://|^git@`)

// fetch resolves src to a local directory: a clone URL is shallow-cloned to a temp dir (removed
// by cleanup), a local path is used as is.
func (a *Aggregator) fetch(src string) (string, func(), error) {
	if !cloneURL.MatchString(src) {
		return src, func() {}, nil
	}
	tmp, err := os.MkdirTemp("", "relai-team-*")
	if err != nil {
		return "", func() {}, err
	}
	if _, err := a.runNet(src, "clone", "--depth", "1", "--quiet", src, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", func() {}, fmt.Errorf("clone %s: %w", src, err)
	}
	return tmp, func() { os.RemoveAll(tmp) }, nil
}

func isDir(p string) bool { st, err := os.Stat(p); return err == nil && st.IsDir() }
