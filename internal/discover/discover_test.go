package discover

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestWorktreesAreOneRepository(t *testing.T) {
	root := t.TempDir()
	main := filepath.Join(root, "app")
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, main, "init", "-q")
	gitRun(t, main, "commit", "-q", "--allow-empty", "-m", "x")
	gitRun(t, main, "worktree", "add", "-q", filepath.Join(root, "app-feature"), "-b", "feature")
	got, err := Find(root, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != main {
		t.Fatalf("want only %s, got %v", main, got)
	}
}

func TestClonesOfOneRemoteAreNotALink(t *testing.T) {
	root := t.TempDir()
	var repos []Repo
	for _, n := range []string{"alpha-one", "alpha-two"} {
		d := filepath.Join(root, n)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		gitRun(t, d, "init", "-q")
		gitRun(t, d, "remote", "add", "origin", "https://example.com/acme/alpha.git")
		if err := os.WriteFile(filepath.Join(d, "README.md"), []byte("see acme/alpha and alpha-one alpha-two\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		gitRun(t, d, "add", ".")
		gitRun(t, d, "commit", "-q", "-m", "x")
		repos = append(repos, Repo{ID: n, Path: d})
	}
	if ls := Mentions(repos); len(ls) != 0 {
		t.Fatalf("clones of one remote must not link, got %+v", ls)
	}
}
