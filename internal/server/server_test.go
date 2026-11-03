package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/board"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
)

// fixture is a server over one fixture repo ("billing-api") whose Beads output the test controls.
type fixture struct {
	t       *testing.T
	repoDir string
	logPath string
	cfg     Config
	mu      sync.Mutex
	bd      string // canned `bd list --json` output
	tick    int
	log     *store.Log
	srv     *Server
	http    *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	f := &fixture{
		t:       t,
		repoDir: filepath.Join(root, "billing"),
		logPath: filepath.Join(root, "data", "events.jsonl"),
		bd: `[{"id":"b-1","title":"Wire the PDF endpoint","status":"open","issue_type":"task"},
		      {"id":"b-2","title":"Document it","status":"open","issue_type":"task"}]`,
	}
	_ = os.MkdirAll(filepath.Join(f.repoDir, ".beads"), 0o755)
	_ = os.WriteFile(filepath.Join(f.repoDir, "relai.yaml"), []byte("id: billing-api\nsummary: Invoicing.\n"), 0o644)
	cmd := exec.Command("git", "init", "-q", "-b", "main")
	cmd.Dir = f.repoDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	_ = os.MkdirAll(filepath.Dir(f.logPath), 0o755)
	sum := sha256.Sum256([]byte("bob-token"))
	f.cfg = Config{
		Repos: []string{f.repoDir},
		Members: []Member{
			{Identity: "alice", Token: "alice-token"},
			{Identity: "root", Token: "root-token", Admin: true},
			{Identity: "bob", TokenSHA256: hex.EncodeToString(sum[:])},
		},
	}
	f.start()
	t.Cleanup(f.stop)
	return f
}

// setBD changes what `bd list --json` answers and, like the real bd does on every write, touches
// .beads/last-touched so the aggregator's fingerprint sees that something changed.
func (f *fixture) setBD(out string) {
	f.t.Helper()
	f.mu.Lock()
	f.bd = out
	f.mu.Unlock()
	f.touchBeads()
}

func (f *fixture) touchBeads() {
	f.t.Helper()
	p := filepath.Join(f.repoDir, ".beads", "last-touched")
	f.tick++
	if err := os.WriteFile(p, []byte(fmt.Sprintf("touch-%d", f.tick)), 0o644); err != nil {
		f.t.Fatal(err)
	}
	when := time.Now().Add(time.Duration(f.tick) * time.Second)
	_ = os.Chtimes(p, when, when)
}

func (f *fixture) runner(dir, name string, args ...string) ([]byte, error) {
	if name == "bd" {
		f.mu.Lock()
		defer f.mu.Unlock()
		return []byte(f.bd), nil
	}
	return aggregate.ExecRunner(dir, name, args...)
}

// start (re)builds the server from the event log, as a process restart would.
func (f *fixture) start() {
	f.t.Helper()
	past, err := store.Replay(f.logPath)
	if err != nil {
		f.t.Fatal(err)
	}
	f.log, err = store.Open(f.logPath)
	if err != nil {
		f.t.Fatal(err)
	}
	f.srv, err = New(f.cfg, board.New(f.log, past), &aggregate.Aggregator{Run: f.runner})
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.srv.Refresh(); err != nil {
		f.t.Fatal(err)
	}
	f.http = httptest.NewServer(f.srv.Handler())
}

func (f *fixture) stop() {
	if f.http != nil {
		f.http.Close()
		f.http = nil
	}
	if f.log != nil {
		f.log.Close()
		f.log = nil
	}
}

func (f *fixture) do(method, path, token string) (int, map[string]any, []any) {
	f.t.Helper()
	req, _ := http.NewRequest(method, f.http.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw json.RawMessage
	_ = json.NewDecoder(resp.Body).Decode(&raw)
	var obj map[string]any
	var arr []any
	if json.Unmarshal(raw, &obj) != nil {
		_ = json.Unmarshal(raw, &arr)
	}
	return resp.StatusCode, obj, arr
}

func itemPath(id, action string) string {
	p := "/items/" + url.PathEscape(id)
	if action != "" {
		p += "/" + action
	}
	return p
}

func TestAuthentication(t *testing.T) {
	f := newFixture(t)
	if code, _, _ := f.do("GET", "/healthz", ""); code != 200 {
		t.Errorf("healthz without token = %d, want 200", code)
	}
	if code, obj, _ := f.do("GET", "/items", ""); code != 401 || obj["error"] == nil {
		t.Errorf("unauthenticated read = %d %v, want 401 with an error and no board data", code, obj)
	}
	if code, _, _ := f.do("GET", "/items", "not-a-member"); code != 401 {
		t.Errorf("unknown token = %d, want 401", code)
	}
	if code, obj, _ := f.do("GET", "/me", "alice-token"); code != 200 || obj["identity"] != "alice" {
		t.Errorf("/me for alice = %d %v", code, obj)
	}
	if code, obj, _ := f.do("GET", "/me", "bob-token"); code != 200 || obj["identity"] != "bob" {
		t.Errorf("/me for bob (token_sha256 member) = %d %v", code, obj)
	}
}

func TestUndeclaredRepoIsNotTracked(t *testing.T) {
	f := newFixture(t)
	code, obj, _ := f.do("GET", "/repos/some-other-repo/items", "alice-token")
	if code != 404 || !strings.Contains(obj["error"].(string), "not tracked") {
		t.Errorf("undeclared repo = %d %v, want 404 'not tracked'", code, obj)
	}
	if code, _, arr := f.do("GET", "/repos/billing-api/items", "alice-token"); code != 200 || len(arr) != 2 {
		t.Errorf("declared repo = %d, %d items", code, len(arr))
	}
}

func TestClaimReleaseRoundTrip(t *testing.T) {
	f := newFixture(t)
	id := "billing-api/bd/b-1"

	if code, obj, _ := f.do("POST", itemPath(id, "claim"), "alice-token"); code != 200 || obj["holder"] != "alice" {
		t.Fatalf("alice claim = %d %v", code, obj)
	}
	if code, obj, _ := f.do("POST", itemPath(id, "claim"), "bob-token"); code != 409 || obj["holder"] != "alice" {
		t.Errorf("bob claiming alice's item = %d %v, want 409 naming alice", code, obj)
	}
	if code, obj, _ := f.do("POST", itemPath(id, "release"), "bob-token"); code != 409 || obj["holder"] != "alice" {
		t.Errorf("bob releasing alice's item = %d %v, want 409", code, obj)
	}
	if code, _, arr := f.do("GET", "/items?holder=me", "alice-token"); code != 200 || len(arr) != 1 {
		t.Errorf("alice's items = %d %v", code, arr)
	}
	if code, _, _ := f.do("POST", itemPath(id, "release"), "alice-token"); code != 200 {
		t.Fatalf("alice release = %d", code)
	}
	if code, obj, _ := f.do("GET", itemPath(id, ""), "bob-token"); code != 200 || obj["holder"] != nil {
		t.Errorf("released item, seen by bob = %d %v (release must be visible to every client)", code, obj)
	}
	if code, _, _ := f.do("POST", itemPath(id, "claim"), "bob-token"); code != 200 {
		t.Errorf("bob claiming the released item = %d, want 200", code)
	}
	if code, _, _ := f.do("POST", itemPath("billing-api/bd/nope", "claim"), "bob-token"); code != 404 {
		t.Errorf("unknown item = %d, want 404", code)
	}
}

func TestTaskStatusChangeShowsAfterRefresh(t *testing.T) {
	f := newFixture(t)
	f.setBD(`[{"id":"b-1","title":"Wire the PDF endpoint","status":"in_progress","issue_type":"task"},
	         {"id":"b-2","title":"Document it","status":"closed","issue_type":"task"}]`)
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	_, obj, _ := f.do("GET", itemPath("billing-api/bd/b-1", ""), "alice-token")
	if obj["status"] != "in_progress" {
		t.Errorf("status after refresh = %v, want in_progress", obj["status"])
	}
	if code, _, _ := f.do("GET", itemPath("billing-api/bd/b-2", ""), "alice-token"); code != 404 {
		t.Errorf("closed task still on the board (%d)", code)
	}
}

func TestRestartKeepsHeldItemEvenIfRepoIsUnreadable(t *testing.T) {
	f := newFixture(t)
	id := "billing-api/bd/b-1"
	if code, _, _ := f.do("POST", itemPath(id, "claim"), "alice-token"); code != 200 {
		t.Fatal("claim failed")
	}
	f.stop()

	// The repo is gone when the server comes back.
	if err := os.RemoveAll(f.repoDir); err != nil {
		t.Fatal(err)
	}
	f.start()

	code, obj, _ := f.do("GET", itemPath(id, ""), "bob-token")
	if code != 200 || obj["holder"] != "alice" {
		t.Errorf("after restart = %d %v, want the item still held by alice", code, obj)
	}
	_, _, repos := f.do("GET", "/repos", "bob-token")
	if len(repos) != 1 {
		t.Fatalf("repos = %v", repos)
	}
	st := repos[0].(map[string]any)
	if st["readable"] != false || st["id"] != "billing-api" || st["error"] != "not found" {
		t.Errorf("repo status after restart = %v", st)
	}
}

func TestLoadConfigValidation(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "c.json")
		_ = os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	cfg, err := LoadConfig(write(`{"repos":["/r"],"members":[{"identity":"a","token":"t"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" || cfg.DataDir != "data" || cfg.RefreshInterval != "30s" {
		t.Errorf("defaults not applied: %+v", cfg)
	}
	for name, body := range map[string]string{
		"no members":     `{"repos":[]}`,
		"no identity":    `{"members":[{"token":"t"}]}`,
		"dup identity":   `{"members":[{"identity":"a","token":"t"},{"identity":"a","token":"u"}]}`,
		"dup token":      `{"members":[{"identity":"a","token":"t"},{"identity":"b","token":"t"}]}`,
		"both":           `{"members":[{"identity":"a","token":"t","token_sha256":"00"}]}`,
		"bad sha":        `{"members":[{"identity":"a","token_sha256":"zz"}]}`,
		"no token":       `{"members":[{"identity":"a"}]}`,
		"bad interval":   `{"refresh_interval":"soon","members":[{"identity":"a","token":"t"}]}`,
		"short interval": `{"refresh_interval":"10ms","members":[{"identity":"a","token":"t"}]}`,
	} {
		if _, err := LoadConfig(write(body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}
