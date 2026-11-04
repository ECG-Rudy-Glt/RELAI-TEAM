package mcp

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/board"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/server"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/store"
)

// startServer runs a real relai-server in process over one fixture repo with n open tasks.
func startServer(t *testing.T, n int) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "billing")
	_ = os.MkdirAll(filepath.Join(repo, ".beads"), 0o755)
	_ = os.WriteFile(filepath.Join(repo, "relai.yaml"), []byte("id: billing-api\nsummary: Invoicing.\n"), 0o644)
	if out, err := exec.Command("git", "-C", repo, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	var tasks []map[string]string
	for i := 1; i <= n; i++ {
		tasks = append(tasks, map[string]string{"id": fmt.Sprintf("b-%d", i), "title": fmt.Sprintf("Task %d", i), "status": "open", "issue_type": "task"})
	}
	tasks = append(tasks, map[string]string{"id": "blk", "title": "Blocked one", "status": "blocked", "issue_type": "task"})
	canned, _ := json.Marshal(tasks)
	run := func(dir, name string, args ...string) ([]byte, error) {
		if name == "bd" {
			return canned, nil
		}
		return aggregate.ExecRunner(dir, name, args...)
	}
	logPath := filepath.Join(root, "data", "events.jsonl")
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	lg, err := store.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lg.Close() })
	cfg := server.Config{ClaimLease: "8h", Repos: []string{repo}, Members: []server.Member{
		{Identity: "alice", Token: "alice-token"}, {Identity: "bob", Token: "bob-token"},
		{Identity: "carol", Token: "carol-token", Admin: true}, {Identity: "dave", Token: "dave-token"},
	}}
	srv, err := server.New(cfg, board.New(lg, nil), &aggregate.Aggregator{Run: run})
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	h := httptest.NewServer(srv.Handler())
	t.Cleanup(h.Close)
	return h
}

func tool(t *testing.T, c *Client, name string, args string) (string, bool) {
	t.Helper()
	for _, tl := range Tools(c) {
		if tl.Name == name {
			return tl.Handler(json.RawMessage(args))
		}
	}
	t.Fatalf("no tool %s", name)
	return "", false
}

func TestListClaimAttributedToCaller(t *testing.T) {
	h := startServer(t, 3)
	alice := &Client{BaseURL: h.URL, Token: "alice-token"}

	txt, isErr := tool(t, alice, "board_items", `{"holder":"none"}`)
	if isErr || !strings.HasPrefix(txt, "4 items") || !strings.Contains(txt, "billing-api/bd/b-1 [open] holder=-") {
		t.Fatalf("items = %v %q", isErr, txt)
	}
	txt, isErr = tool(t, alice, "board_claim", `{"id":"billing-api/bd/b-1"}`) // slashes sent verbatim
	if isErr || !strings.Contains(txt, "holder=alice") {
		t.Fatalf("claim = %v %q", isErr, txt)
	}
	txt, _ = tool(t, alice, "board_items", `{"holder":"me"}`)
	if !strings.HasPrefix(txt, "1 items") {
		t.Errorf("my items = %q", txt)
	}
}

func TestContentionIsAResultNotACrash(t *testing.T) {
	h := startServer(t, 2)
	alice := &Client{BaseURL: h.URL, Token: "alice-token"}
	bob := &Client{BaseURL: h.URL, Token: "bob-token"}
	tool(t, alice, "board_claim", `{"id":"billing-api/bd/b-1"}`)

	txt, isErr := tool(t, bob, "board_claim", `{"id":"billing-api/bd/b-1"}`)
	if !isErr || !strings.Contains(txt, "alice") {
		t.Errorf("lost race = %v %q, want an error result naming alice", isErr, txt)
	}
	txt, isErr = tool(t, bob, "board_release", `{"id":"billing-api/bd/b-1"}`)
	if !isErr || !strings.Contains(txt, "alice") {
		t.Errorf("release of alice's item = %v %q", isErr, txt)
	}
	txt, _ = tool(t, alice, "board_items", `{"holder":"alice"}`)
	if !strings.Contains(txt, "holder=alice") {
		t.Errorf("holder changed: %q", txt)
	}
	if txt, _ = tool(t, alice, "board_items", `{"holder":"alice"}`); !strings.Contains(txt, "holder=alice (you)") {
		t.Errorf("own claims must be marked (you): %q", txt)
	}
	if txt, _ = tool(t, &Client{BaseURL: h.URL, Token: "bob-token"}, "board_items", `{"holder":"alice"}`); strings.Contains(txt, "(you)") {
		t.Errorf("someone else's claim must not be marked (you): %q", txt)
	}
	if txt, isErr = tool(t, alice, "board_release", `{"id":"billing-api/bd/b-1"}`); isErr {
		t.Errorf("holder release failed: %q", txt)
	}
}

func TestUnknownTokenAndSecrecy(t *testing.T) {
	h := startServer(t, 1)
	bad := &Client{BaseURL: h.URL, Token: "s3cret-nope"}
	for _, name := range []string{"board_repos", "board_items", "board_claim_next"} {
		txt, isErr := tool(t, bad, name, `{}`)
		if !isErr || !strings.Contains(txt, "authentication failed") || strings.Contains(txt, "s3cret-nope") {
			t.Errorf("%s = %v %q", name, isErr, txt)
		}
	}
}

func TestServerDownThenBack(t *testing.T) {
	h := startServer(t, 1)
	c := &Client{BaseURL: h.URL, Token: "alice-token"}
	h.CloseClientConnections()
	url := h.URL
	h.Close()
	txt, isErr := tool(t, c, "board_repos", `{}`)
	if !isErr || !strings.Contains(txt, "unreachable") {
		t.Errorf("down = %v %q", isErr, txt)
	}
	h2 := startServer(t, 1)
	c.BaseURL = h2.URL
	if txt, isErr = tool(t, c, "board_repos", `{}`); isErr {
		t.Errorf("after recovery = %q (was %s)", txt, url)
	}
}

func TestClaimNextSkipsBlockedAndEmpties(t *testing.T) {
	h := startServer(t, 1)
	a := &Client{BaseURL: h.URL, Token: "alice-token"}
	b := &Client{BaseURL: h.URL, Token: "bob-token"}
	if txt, isErr := tool(t, a, "board_claim_next", `{}`); isErr || !strings.Contains(txt, "b-1") {
		t.Fatalf("first = %v %q", isErr, txt)
	}
	txt, isErr := tool(t, b, "board_claim_next", `{}`)
	if isErr || !strings.Contains(txt, "No unclaimed") || strings.Contains(txt, "blk") {
		t.Errorf("nothing available = %v %q (blocked item must not be claimed)", isErr, txt)
	}
}

func TestClaimNextConcurrentAgentsGetDistinctItems(t *testing.T) {
	const agents = 4
	h := startServer(t, agents)
	toks := []string{"alice-token", "bob-token", "carol-token", "dave-token"}
	got := make([]string, agents)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < agents; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			txt, isErr := tool(t, &Client{BaseURL: h.URL, Token: toks[i]}, "board_claim_next", `{}`)
			if isErr {
				t.Errorf("agent %d: %q", i, txt)
			}
			got[i] = txt
		}(i)
	}
	close(start)
	wg.Wait()
	seen := map[string]bool{}
	for i, g := range got {
		id := strings.Fields(strings.TrimPrefix(g, "Claimed: "))[0]
		if !strings.Contains(g, "holder="+strings.TrimSuffix(toks[i], "-token")) {
			t.Errorf("agent %d got %q, not attributed to itself", i, g)
		}
		if seen[id] {
			t.Errorf("item %s given to two agents", id)
		}
		seen[id] = true
	}
	if len(seen) != agents {
		t.Errorf("distinct items = %d, want %d", len(seen), agents)
	}
}

func TestBadArgumentsAreErrorsNotSilentEmptyAnswers(t *testing.T) {
	h := startServer(t, 2)
	alice := &Client{BaseURL: h.URL, Token: "alice-token"}
	for name, c := range map[string]struct{ tool, args, want string }{
		"unknown status":       {"board_items", `{"status":"bogus"}`, "unknown status"},
		"unknown repo in list": {"board_items", `{"repo":"nope"}`, `unknown repo "nope"`},
		"unknown repo in next": {"board_claim_next", `{"repo":"nope"}`, "billing-api"},
		"negative limit":       {"board_items", `{"limit":-3}`, "positive whole number"},
		"zero limit":           {"board_items", `{"limit":0}`, "positive whole number"},
		"string limit":         {"board_items", `{"limit":"x"}`, "positive whole number"},
	} {
		txt, isErr := tool(t, alice, c.tool, c.args)
		if !isErr || !strings.Contains(txt, c.want) {
			t.Errorf("%s: got isErr=%v %q, want an error mentioning %q", name, isErr, txt, c.want)
		}
	}
	if txt, isErr := tool(t, alice, "board_items", `{"limit":1,"status":"open","repo":"billing-api"}`); isErr || !strings.HasPrefix(txt, "2 items") {
		t.Errorf("valid arguments refused: %v %q", isErr, txt)
	}
	// claim_next must not have claimed anything for the unknown repo.
	if txt, _ := tool(t, alice, "board_items", `{"holder":"me"}`); !strings.HasPrefix(txt, "0 items") {
		t.Errorf("an invalid claim_next claimed something: %q", txt)
	}
}

func TestLapseTimeIncludesTheDate(t *testing.T) {
	h := startServer(t, 1)
	alice := &Client{BaseURL: h.URL, Token: "alice-token"}
	txt, _ := tool(t, alice, "board_claim", `{"id":"billing-api/bd/b-1"}`)
	if !regexp.MustCompile(`lapses \d{4}-\d{2}-\d{2} \d{2}:\d{2}Z`).MatchString(txt) {
		t.Errorf("the lapse time should carry a date: %q", txt)
	}
}

func TestBoardChangesGivesACursorAndOnlyWhatIsNew(t *testing.T) {
	h := startServer(t, 3)
	alice := &Client{BaseURL: h.URL, Token: "alice-token"}
	bob := &Client{BaseURL: h.URL, Token: "bob-token"}

	txt, isErr := tool(t, alice, "board_changes", `{}`)
	m := regexp.MustCompile(`board seq (\d+)`).FindStringSubmatch(txt)
	if isErr || m == nil || !strings.Contains(txt, "next: board_changes since="+m[1]) {
		t.Fatalf("first call = %v %q", isErr, txt)
	}
	tool(t, bob, "board_claim", `{"id":"billing-api/bd/b-1"}`)
	txt, isErr = tool(t, alice, "board_changes", `{"since":`+m[1]+`}`)
	if isErr || !strings.Contains(txt, "1 changes") || !strings.Contains(txt, "billing-api/bd/b-1 claimed by bob") {
		t.Fatalf("after bob's claim = %v %q", isErr, txt)
	}
	if strings.Contains(txt, "b-2") {
		t.Errorf("only what changed should be listed: %q", txt)
	}
	m2 := regexp.MustCompile(`next: board_changes since=(\d+)`).FindStringSubmatch(txt)
	txt, _ = tool(t, alice, "board_changes", `{"since":`+m2[1]+`}`)
	if !strings.Contains(txt, "0 changes") {
		t.Errorf("nothing new: %q", txt)
	}
	if txt, isErr = tool(t, alice, "board_changes", `{"since":-1}`); !isErr {
		t.Errorf("a negative since must be refused: %q", txt)
	}
}
