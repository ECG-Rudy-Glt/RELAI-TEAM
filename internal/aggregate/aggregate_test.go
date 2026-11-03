package aggregate

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"testing"
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
