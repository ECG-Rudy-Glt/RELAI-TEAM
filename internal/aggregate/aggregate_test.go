package aggregate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

// gitRepo creates a git repo at dir with the given files.
func gitRepo(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
}

// fakeRunner runs git for real and answers bd/openspec from canned JSON keyed by repo dir.
func fakeRunner(bd, openspec map[string]string) Runner {
	return func(dir, name string, args ...string) ([]byte, error) {
		switch name {
		case "bd":
			return []byte(bd[dir]), nil
		case "openspec":
			return []byte(openspec[dir]), nil
		}
		return ExecRunner(dir, name, args...)
	}
}

const tasksMD = `# Tasks

## 1. Thing

- [x] 1.1 Done already
- [ ] 1.2 Wire the endpoint
- [ ] 1.3 Document it
`

func TestOne_FullContract(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "billing")
	gitRepo(t, dir, map[string]string{
		"relai.yaml":                           "apiVersion: relai.dev/v0\nid: billing-api\nsummary: \"Invoicing.\"   # comment\n",
		".beads/config.yaml":                   "x",
		"openspec/changes/pdf-export/tasks.md": tasksMD,
	})
	a := &Aggregator{Run: fakeRunner(
		map[string]string{dir: `[
			{"id":"b-1","title":"Open task","status":"open","issue_type":"task"},
			{"id":"b-2","title":"Done task","status":"closed","issue_type":"task"},
			{"id":"b-3","title":"An epic","status":"open","issue_type":"epic"},
			{"id":"b-4","title":"Busy task","status":"in_progress","issue_type":"task"}]`},
		map[string]string{dir: `{"changes":[{"name":"pdf-export"}]}`},
	)}

	r, err := a.One(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "billing-api" || r.Summary != "Invoicing." {
		t.Errorf("manifest: id=%q summary=%q", r.ID, r.Summary)
	}
	var ids []string
	for _, it := range r.Items {
		ids = append(ids, it.ID)
	}
	sort.Strings(ids)
	want := []string{
		"billing-api/bd/b-1",
		"billing-api/bd/b-4",
		"billing-api/openspec/pdf-export/1.2",
		"billing-api/openspec/pdf-export/1.3",
	}
	if len(ids) != len(want) {
		t.Fatalf("items = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("item %d = %q, want %q", i, ids[i], want[i])
		}
	}
	if len(r.Notes) != 0 {
		t.Errorf("unexpected notes: %v", r.Notes)
	}
}

func TestOne_NoBeadsNoOpenSpecNoManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bare-repo")
	gitRepo(t, dir, map[string]string{"README.md": "hi"})
	a := &Aggregator{Run: fakeRunner(nil, nil)}
	r, err := a.One(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID != "bare-repo" || len(r.Items) != 0 {
		t.Errorf("bare repo: %+v", r)
	}
	if len(r.Notes) != 1 || r.Notes[0] != "no relai.yaml" {
		t.Errorf("notes = %v", r.Notes)
	}
}

func TestOne_MissingPathAndNotARepo(t *testing.T) {
	root := t.TempDir()
	a := &Aggregator{Run: fakeRunner(nil, nil)}
	if _, err := a.One(filepath.Join(root, "nope")); err == nil || err.Error() != "not found" {
		t.Errorf("missing path: %v", err)
	}
	plain := filepath.Join(root, "plain")
	_ = os.MkdirAll(plain, 0o755)
	if _, err := a.One(plain); err == nil || err.Error() != "not a git repo" {
		t.Errorf("not a repo: %v", err)
	}
}

func TestOne_BrokenBeadsIsANoteNotAFailure(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "r")
	gitRepo(t, dir, map[string]string{"relai.yaml": "id: r\n", ".beads/x": "x"})
	a := &Aggregator{Run: fakeRunner(map[string]string{dir: "not json"}, nil)}
	r, err := a.One(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Notes) != 1 {
		t.Errorf("want one note about beads, got %v", r.Notes)
	}
}

func TestAll_MultiRepo(t *testing.T) {
	root := t.TempDir()
	auth := filepath.Join(root, "auth")
	billing := filepath.Join(root, "billing")
	gitRepo(t, auth, map[string]string{"relai.yaml": "id: auth-service\n", ".beads/x": "x"})
	gitRepo(t, billing, map[string]string{
		"relai.yaml":                    "id: billing-api\n",
		"openspec/changes/pdf/tasks.md": tasksMD,
	})
	a := &Aggregator{Run: fakeRunner(
		map[string]string{auth: `[{"id":"a-1","title":"Add MFA","status":"open","issue_type":"task"}]`},
		map[string]string{billing: `{"changes":[{"name":"pdf"}]}`},
	)}

	repos, skipped := a.All([]string{auth, billing, filepath.Join(root, "gone")})
	if len(repos) != 2 || len(skipped) != 1 || skipped[0].Reason != "not found" {
		t.Fatalf("repos=%d skipped=%+v", len(repos), skipped)
	}
	items := Items(repos)
	if len(items) != 3 {
		t.Fatalf("items = %+v", items)
	}
	got := map[string]Item{}
	for _, it := range items {
		got[it.ID] = it
	}
	if it := got["auth-service/bd/a-1"]; it.Repo != "auth-service" || it.Kind != KindTask || it.Title != "Add MFA" {
		t.Errorf("auth item = %+v", it)
	}
	if it := got["billing-api/openspec/pdf/1.2"]; it.Kind != KindChangeTask || it.Title != "pdf: 1.2 Wire the endpoint" {
		t.Errorf("billing item = %+v", it)
	}
}

func TestOpenTasksUnnumbered(t *testing.T) {
	got := openTasks("- [x] first\n- [ ] second thing\n  - [ ] 2.1 nested numbered\n")
	if len(got) != 2 || got[0].num != "t2" || got[1].num != "2.1" {
		t.Fatalf("got %+v", got)
	}
}

// slowRunner answers git for real, and bd/openspec after a fixed delay, tracking how many calls
// run at once and how many times each directory is being read at the same moment.
type slowRunner struct {
	delay      time.Duration
	mu         sync.Mutex
	inflight   int
	maxInfl    int
	perDir     map[string]int
	dirOverlap bool
}

func (r *slowRunner) run(dir, name string, args ...string) ([]byte, error) {
	if name != "bd" {
		return ExecRunner(dir, name, args...)
	}
	r.mu.Lock()
	r.inflight++
	if r.inflight > r.maxInfl {
		r.maxInfl = r.inflight
	}
	if r.perDir == nil {
		r.perDir = map[string]int{}
	}
	r.perDir[dir]++
	if r.perDir[dir] > 1 {
		r.dirOverlap = true
	}
	r.mu.Unlock()
	time.Sleep(r.delay)
	r.mu.Lock()
	r.inflight--
	r.perDir[dir]--
	r.mu.Unlock()
	return []byte(`[{"id":"x-1","title":"t","status":"open","issue_type":"task"}]`), nil
}

func manyRepos(t *testing.T, n int) []string {
	t.Helper()
	root := t.TempDir()
	var srcs []string
	for i := 0; i < n; i++ {
		d := filepath.Join(root, fmt.Sprintf("repo-%02d", i))
		gitRepo(t, d, map[string]string{"relai.yaml": fmt.Sprintf("id: repo-%02d\n", i), ".beads/x": "x"})
		srcs = append(srcs, d)
	}
	return srcs
}

func TestAll_ReadsReposConcurrentlyAndKeepsOrder(t *testing.T) {
	srcs := manyRepos(t, 10)
	r := &slowRunner{delay: 200 * time.Millisecond}
	a := &Aggregator{Run: r.run}
	start := time.Now()
	repos, skipped := a.All(srcs)
	took := time.Since(start)
	if len(repos) != 10 || len(skipped) != 0 {
		t.Fatalf("repos=%d skipped=%d", len(repos), len(skipped))
	}
	for i, rp := range repos {
		if want := fmt.Sprintf("repo-%02d", i); rp.ID != want {
			t.Errorf("repo %d = %s, want %s (declared order must be kept)", i, rp.ID, want)
		}
	}
	// Sequentially this is >= 10 * 200ms = 2s. Concurrently it is about one delay.
	if took > 1200*time.Millisecond {
		t.Errorf("took %v for 10 repos at 200ms each; reads are not running concurrently", took)
	}
}

func TestAll_WorkerCapIsRespected(t *testing.T) {
	srcs := manyRepos(t, 9)
	r := &slowRunner{delay: 50 * time.Millisecond}
	a := &Aggregator{Run: r.run, Workers: 3}
	a.All(srcs)
	if r.maxInfl > 3 {
		t.Errorf("max concurrent reads = %d, want <= 3", r.maxInfl)
	}
	if r.maxInfl < 2 {
		t.Errorf("max concurrent reads = %d, expected real concurrency", r.maxInfl)
	}
}

func TestAll_BrokenRepoDoesNotHoldBackTheOthers(t *testing.T) {
	srcs := manyRepos(t, 4)
	srcs = append(srcs[:2], append([]string{"/nonexistent/path"}, srcs[2:]...)...)
	r := &slowRunner{delay: 10 * time.Millisecond}
	repos, skipped := (&Aggregator{Run: r.run}).All(srcs)
	if len(repos) != 4 || len(skipped) != 1 || skipped[0].Source != "/nonexistent/path" {
		t.Fatalf("repos=%d skipped=%v", len(repos), skipped)
	}
}

func TestAll_SameSourceTwiceIsReadOnceAndNeverConcurrently(t *testing.T) {
	srcs := manyRepos(t, 1)
	r := &slowRunner{delay: 50 * time.Millisecond}
	repos, _ := (&Aggregator{Run: r.run}).All([]string{srcs[0], srcs[0], srcs[0]})
	if len(repos) != 1 {
		t.Errorf("repos = %d, want 1 (duplicates collapsed)", len(repos))
	}
	if r.dirOverlap {
		t.Error("the same repo was read by two workers at once")
	}
}
