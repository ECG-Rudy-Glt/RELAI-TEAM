package aggregate

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// counter wraps a runner and counts the commands that matter for disk writes and network use.
type counter struct {
	inner          Runner
	bd, openspec   atomic.Int32
	clone, lsRemte atomic.Int32
	bdErr          error
}

func (c *counter) run(dir, name string, args ...string) ([]byte, error) {
	switch {
	case name == "bd":
		c.bd.Add(1)
		if c.bdErr != nil {
			return nil, c.bdErr
		}
	case name == "openspec":
		c.openspec.Add(1)
	case name == "git" && len(args) > 0 && args[0] == "clone":
		c.clone.Add(1)
	case name == "git" && len(args) > 0 && args[0] == "ls-remote":
		c.lsRemte.Add(1)
	}
	return c.inner(dir, name, args...)
}

const oneTask = `[{"id":"t-1","title":"A task","status":"open","issue_type":"task"}]`

// cachedRepo builds a repo with Beads (and last-touched, as real bd leaves it) and OpenSpec.
func cachedRepo(t *testing.T) (dir string, c *counter, a *Aggregator) {
	t.Helper()
	dir = t.TempDir()
	gitRepo(t, dir, map[string]string{
		"relai.yaml":                        "id: svc\n",
		".beads/last-touched":               "t-1\n",
		"openspec/changes/c1/tasks.md":      tasksMD,
		"openspec/changes/c1/proposal.md":   "# p\n",
		"openspec/changes/archive/old/x.md": "old\n",
	})
	c = &counter{inner: fakeRunner(map[string]string{dir: oneTask}, map[string]string{dir: `{"changes":[{"name":"c1"}]}`})}
	a = &Aggregator{Run: c.run}
	return dir, c, a
}

func bump(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	_ = os.Chtimes(path, later, later)
}

func TestCache_UnchangedRepoSpawnsNothing(t *testing.T) {
	dir, c, a := cachedRepo(t)
	first, _ := a.All([]string{dir})
	for i := 0; i < 50; i++ { // a refresh every 30 s for 25 minutes
		again, skipped := a.All([]string{dir})
		if len(skipped) != 0 || len(again) != 1 || len(again[0].Items) != len(first[0].Items) {
			t.Fatalf("cycle %d: %v %v", i, again, skipped)
		}
	}
	if c.bd.Load() != 1 || c.openspec.Load() != 1 {
		t.Errorf("bd ran %d times and openspec %d times for 51 cycles, want 1 and 1 (every bd run rewrites its database)", c.bd.Load(), c.openspec.Load())
	}
	if a.Reads() != 1 || a.CacheHits() != 50 {
		t.Errorf("reads=%d hits=%d, want 1 and 50", a.Reads(), a.CacheHits())
	}
}

func TestCache_BeadsWriteIsSeenImmediately(t *testing.T) {
	dir, c, a := cachedRepo(t)
	a.All([]string{dir})
	bump(t, filepath.Join(dir, ".beads", "last-touched"), "t-2\n") // what any bd write does
	a.All([]string{dir})
	if c.bd.Load() != 2 {
		t.Errorf("bd ran %d times, want 2: a change in Beads must trigger a re-read", c.bd.Load())
	}
}

func TestCache_OpenSpecEditIsSeen_ArchiveIsNot(t *testing.T) {
	dir, c, a := cachedRepo(t)
	a.All([]string{dir})
	bump(t, filepath.Join(dir, "openspec", "changes", "archive", "old", "x.md"), "still old\n")
	a.All([]string{dir})
	if c.openspec.Load() != 1 {
		t.Errorf("an edit under changes/archive re-read the repo (%d reads); archived changes produce no items", c.openspec.Load())
	}
	bump(t, filepath.Join(dir, "openspec", "changes", "c1", "tasks.md"), strings.Replace(tasksMD, "- [ ] 1.2", "- [x] 1.2", 1))
	r, _ := a.All([]string{dir})
	if c.openspec.Load() != 2 {
		t.Errorf("ticking a task did not trigger a re-read (%d)", c.openspec.Load())
	}
	for _, it := range r[0].Items {
		if strings.Contains(it.Title, "1.2") || it.ID == "svc/openspec/c1/1.2" {
			t.Errorf("ticked task still listed: %+v", it)
		}
	}
}

func TestCache_RelaiYAMLChangeIsSeen(t *testing.T) {
	dir, _, a := cachedRepo(t)
	a.All([]string{dir})
	bump(t, filepath.Join(dir, "relai.yaml"), "id: renamed\nsummary: new\n")
	r, _ := a.All([]string{dir})
	if r[0].ID != "renamed" {
		t.Errorf("id = %q, want renamed", r[0].ID)
	}
}

func TestCache_SafetyNetRereadsAfterFullRefresh(t *testing.T) {
	dir, c, a := cachedRepo(t)
	a.FullRefresh = 60 * time.Millisecond
	a.All([]string{dir})
	a.All([]string{dir})
	if c.bd.Load() != 1 {
		t.Fatalf("bd ran %d times before the safety net, want 1", c.bd.Load())
	}
	time.Sleep(90 * time.Millisecond)
	a.All([]string{dir})
	if c.bd.Load() != 2 {
		t.Errorf("bd ran %d times, want 2 after FullRefresh elapsed", c.bd.Load())
	}
}

func TestCache_NoLastTouchedStillOnlyReadOnce(t *testing.T) {
	dir, c, a := cachedRepo(t)
	_ = os.Remove(filepath.Join(dir, ".beads", "last-touched"))
	for i := 0; i < 5; i++ {
		a.All([]string{dir})
	}
	if c.bd.Load() != 1 {
		t.Errorf("bd ran %d times without last-touched, want 1 (then only every FullRefresh)", c.bd.Load())
	}
}

func TestCache_BrokenBeadsIsNotRetriedEveryCycle(t *testing.T) {
	dir, c, a := cachedRepo(t)
	c.bdErr = errors.New("exit status 1")
	for i := 0; i < 6; i++ {
		r, _ := a.All([]string{dir})
		if len(r[0].Notes) == 0 {
			t.Fatal("the failure should still be reported as a note")
		}
	}
	if c.bd.Load() != 1 {
		t.Errorf("a failing bd ran %d times in 6 cycles, want 1: retrying in a loop is what burned the disk", c.bd.Load())
	}
}

func TestCache_NoCacheModeAlwaysReads(t *testing.T) {
	dir, c, a := cachedRepo(t)
	a.NoCache = true
	for i := 0; i < 3; i++ {
		a.All([]string{dir})
	}
	if c.bd.Load() != 3 {
		t.Errorf("bd ran %d times in NoCache mode, want 3", c.bd.Load())
	}
}

func TestCache_UnreadableRepoIsNotCachedAndRecovers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "later")
	c := &counter{inner: fakeRunner(map[string]string{dir: oneTask}, nil)}
	a := &Aggregator{Run: c.run}
	if _, sk := a.All([]string{dir}); len(sk) != 1 {
		t.Fatalf("missing repo should be skipped: %v", sk)
	}
	gitRepo(t, dir, map[string]string{"relai.yaml": "id: later\n", ".beads/last-touched": "x"})
	if r, sk := a.All([]string{dir}); len(sk) != 0 || len(r) != 1 {
		t.Errorf("repo that appeared later: repos=%d skipped=%v", len(r), sk)
	}
}

func TestCache_CloneURLUsesLsRemoteAndClonesOnlyOnChange(t *testing.T) {
	origin := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = origin
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(origin, "relai.yaml"), []byte("id: remote-svc\n"), 0o644)
	git("add", "-A")
	git("commit", "-q", "-m", "one")
	url := "file://" + origin

	c := &counter{inner: fakeRunner(nil, nil)}
	a := &Aggregator{Run: c.run}
	for i := 0; i < 5; i++ {
		r, sk := a.All([]string{url})
		if len(sk) != 0 || len(r) != 1 || r[0].ID != "remote-svc" {
			t.Fatalf("cycle %d: %v %v", i, r, sk)
		}
	}
	if c.clone.Load() != 1 {
		t.Errorf("cloned %d times in 5 cycles, want 1 (a clone writes the whole repo to disk each time)", c.clone.Load())
	}
	if c.lsRemte.Load() != 5 {
		t.Errorf("ls-remote ran %d times, want 5 (the cheap check each cycle)", c.lsRemte.Load())
	}
	_ = os.WriteFile(filepath.Join(origin, "relai.yaml"), []byte("id: remote-svc-v2\n"), 0o644)
	git("commit", "-aq", "-m", "two")
	r, _ := a.All([]string{url})
	if c.clone.Load() != 2 || r[0].ID != "remote-svc-v2" {
		t.Errorf("after a new commit: clones=%d id=%s, want 2 and remote-svc-v2", c.clone.Load(), r[0].ID)
	}
}

func TestCache_ConcurrentUseIsSafe(t *testing.T) {
	dir, _, a := cachedRepo(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a.All([]string{dir})
		}()
	}
	wg.Wait()
}
