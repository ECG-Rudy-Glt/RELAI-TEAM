package server

import (
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mkProject(t *testing.T, root, name, remote string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, name)
	_ = os.MkdirAll(dir, 0o755)
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if remote != "" {
		run("remote", "add", "origin", remote)
	}
	for p, body := range files {
		_ = os.MkdirAll(filepath.Dir(filepath.Join(dir, p)), 0o755)
		_ = os.WriteFile(filepath.Join(dir, p), []byte(body), 0o644)
	}
	run("add", "-A")
	run("commit", "-q", "-m", "x")
}

// clusterFixture starts a server fed ONLY by a discover folder: no repos list.
func clusterFixture(t *testing.T) (*fixture, string) {
	f := newFixture(t)
	root := t.TempDir()
	mkProject(t, root, "auth-service", "git@github.com:acme/auth-service.git", map[string]string{
		"relai.yaml": "id: auth-service\nsummary: Sign-in.\nprovides: [{capability: auth.oidc}]\n"})
	mkProject(t, root, "billing", "", map[string]string{
		"relai.yaml": "id: billing-api\nsummary: Invoices.\nprovides:\n  - capability: invoice.pdf\nconsumes:\n  - capability: auth.oidc\nlinks: [web-shop]\n"})
	mkProject(t, root, "web-shop", "", map[string]string{
		"relai.yaml":                   "id: web-shop\nsummary: The shop.\n",
		".github/workflows/deploy.yml": "run: deploy billing\n",
		"k8s/app.yaml":                 "image: billing\n",
		"docs/notes.md":                "see ../auth-service/ for sign-in\n"})
	mkProject(t, root, "plain", "", map[string]string{"README.md": "no relai.yaml here"})
	f.stop()
	f.cfg.Repos = nil
	f.cfg.Discover = []DiscoverConfig{{Folder: root}}
	f.start()
	return f, root
}

func TestTheBoardIsFedFromAFolderWithoutAList(t *testing.T) {
	f, root := clusterFixture(t)
	_, _, repos := f.do("GET", "/repos", "alice-token")
	if len(repos) != 4 {
		t.Fatalf("repos = %d, want the 4 found in the folder: %v", len(repos), repos)
	}
	// a project cloned into the folder later appears on the next refresh, with no config change
	mkProject(t, root, "newcomer", "", map[string]string{"relai.yaml": "id: newcomer\nsummary: New.\n"})
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	_, _, repos = f.do("GET", "/repos", "alice-token")
	if len(repos) != 5 {
		t.Errorf("a newly cloned project should appear by itself: %d repos", len(repos))
	}
}

func TestLinksShowDeclaredEdgesCapabilitiesAndMentionsApart(t *testing.T) {
	f, _ := clusterFixture(t)
	code, obj, _ := f.do("GET", "/links?candidates=1", "alice-token")
	if code != 200 {
		t.Fatalf("links = %d %v", code, obj)
	}
	edges := obj["edges"].([]any)
	has := func(from, to, kind string) bool {
		for _, e := range edges {
			m := e.(map[string]any)
			if m["from"] == from && m["to"] == to && m["kind"] == kind {
				return true
			}
		}
		return false
	}
	if !has("billing-api", "web-shop", "declared") {
		t.Errorf("a links: entry written in relai.yaml must be a declared edge: %v", edges)
	}
	if !has("billing-api", "auth-service", "capability") {
		t.Errorf("consuming auth.oidc must lead to its provider: %v", edges)
	}
	var found *map[string]any
	for _, c := range obj["candidates"].([]any) {
		m := c.(map[string]any)
		if m["from"] == "web-shop" && m["to"] == "billing-api" {
			found = &m
		}
	}
	if found == nil {
		t.Fatalf("web-shop names billing in its deploy files: a candidate was expected: %v", obj["candidates"])
	}
	if (*found)["declared"] != false || (*found)["weak_config_files"].(float64) != 2 {
		t.Errorf("candidate = %v, want declared=false and 2 CI/deploy/config files", *found)
	}
	for _, c := range obj["candidates"].([]any) {
		m := c.(map[string]any)
		if m["from"] == "web-shop" && m["to"] == "auth-service" && m["strong_files"].(float64) < 1 {
			t.Errorf("web-shop -> auth-service holds a relative path: strong evidence expected: %v", m)
		}
	}
	if note, _ := obj["note"].(string); !strings.Contains(note, "not verified") {
		t.Errorf("the answer must say candidates are not verified links: %q", note)
	}
	// without ?candidates=1 no search is made
	_, obj2, _ := f.do("GET", "/links", "alice-token")
	if obj2["candidates"] != nil {
		t.Error("candidates must be opt-in: the search reads every repo")
	}
	if code, _, _ := f.do("GET", "/links", ""); code != 401 {
		t.Errorf("links without a token = %d", code)
	}
}

func TestFindSaysWhatExistsBeforeSomeoneBuildsIt(t *testing.T) {
	f, _ := clusterFixture(t)
	_, obj, _ := f.do("GET", "/find?capability=AUTH.OIDC", "alice-token")
	hits := obj["repos"].([]any)
	roles := map[string]string{}
	for _, h := range hits {
		m := h.(map[string]any)
		roles[m["id"].(string)] = m["role"].(string)
	}
	if roles["auth-service"] != "provides" || roles["billing-api"] != "consumes" {
		t.Errorf("find auth.oidc = %v, want auth-service providing (case-insensitive) and billing-api consuming", roles)
	}
	if _, obj, _ := f.do("GET", "/find?capability=nothing.here", "alice-token"); len(obj["repos"].([]any)) != 0 {
		t.Error("an unknown capability must return no repos")
	}
	if code, _, _ := f.do("GET", "/find", "alice-token"); code != 400 {
		t.Errorf("find without a capability = %d, want 400", code)
	}
}

func TestFindExplainsAnEmptyAnswer(t *testing.T) {
	f, _ := clusterFixture(t)
	_, obj, _ := f.do("GET", "/find?capability=nothing.here", "alice-token")
	if note, _ := obj["note"].(string); !strings.Contains(note, "none of the") {
		t.Errorf("an empty answer needs a note, got %v", obj)
	}
}

func TestClaimsOfAVanishedRepoAreShownAsOrphans(t *testing.T) {
	f, root := clusterFixture(t)
	mkProject(t, root, "ghost", "", map[string]string{"relai.yaml": "id: ghost\nsummary: Soon gone.\n", "TODO.md": "- [ ] [P1] Haunt the house\n"})
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	_, _, items := f.do("GET", "/items", "alice-token")
	var id string
	for _, x := range items {
		m := x.(map[string]any)
		if m["repo"] == "ghost" {
			id = m["id"].(string)
			if m["priority"] != "P1" {
				t.Errorf("the TODO.md priority must reach the API, got %v", m)
			}
		}
	}
	if id == "" {
		t.Fatalf("the TODO.md item of ghost is not on the board: %v", items)
	}
	if code, _, _ := f.do("POST", "/items/"+url.PathEscape(id)+"/claim", "alice-token"); code != 200 {
		t.Fatalf("claim = %d", code)
	}
	if err := os.RemoveAll(filepath.Join(root, "ghost")); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	_, obj, _ := f.do("GET", "/orphans", "alice-token")
	o, _ := obj["orphaned_claims"].([]any)
	if len(o) != 1 || o[0].(map[string]any)["id"] != id {
		t.Errorf("orphans = %v, want the claim on %s", obj, id)
	}
}
