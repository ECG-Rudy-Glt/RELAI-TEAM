package server

import (
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func leasedFixture(t *testing.T) (*fixture, *fakeClock) {
	f := newFixture(t)
	ck := &fakeClock{t: time.Now()}
	f.srv.board.SetClock(ck.now)
	f.srv.board.SetLease(2 * time.Hour)
	return f, ck
}

func TestForce_AdminFreesAndHistoryNamesBoth(t *testing.T) {
	f := newFixture(t)
	id := firstItemID(t, f)
	if c, _, _ := f.do("POST", itemPath(id, "claim"), "bob-token"); c != 200 {
		t.Fatal("claim failed")
	}
	if c, _, _ := f.do("POST", itemPath(id, "release")+"?force=true", "root-token"); c != 200 {
		t.Fatalf("admin force release = %d", c)
	}
	if _, it, _ := f.do("GET", "/items/"+url.PathEscape(id), "alice-token"); it["holder"] != nil {
		t.Errorf("item still held: %v", it)
	}
	raw, _ := os.ReadFile(f.logPath)
	var ok bool
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var e map[string]any
		_ = json.Unmarshal([]byte(line), &e)
		if e["type"] == "item_released" && e["identity"] == "bob" && e["by"] == "root" {
			ok = true
		}
	}
	if !ok {
		t.Errorf("the log does not say bob's claim was forced free by root:\n%s", raw)
	}
}

func TestForce_NonAdminRefusedAndAdminNeedsToAsk(t *testing.T) {
	f := newFixture(t)
	id := firstItemID(t, f)
	f.do("POST", itemPath(id, "claim"), "bob-token")
	if c, body, _ := f.do("POST", itemPath(id, "release")+"?force=true", "alice-token"); c != 403 || !strings.Contains(body["error"].(string), "administrator") {
		t.Errorf("non-admin force = %d %v, want 403 naming administrators", c, body)
	}
	if c, body, _ := f.do("POST", itemPath(id, "release"), "root-token"); c != 409 || body["holder"] != "bob" {
		t.Errorf("admin releasing without force = %d %v, want 409 held by bob", c, body)
	}
	if _, it, _ := f.do("GET", "/items/"+url.PathEscape(id), "alice-token"); it["holder"] != "bob" {
		t.Errorf("holder changed: %v", it)
	}
}

func TestLease_LapseOverHTTPAndNotifiesSubscribers(t *testing.T) {
	f, ck := leasedFixture(t)
	id := firstItemID(t, f)
	_, claimed, _ := f.do("POST", itemPath(id, "claim"), "bob-token")
	if claimed["expires_at"] == nil || claimed["expires_at"] == "" {
		t.Errorf("a held item should say when its claim lapses: %v", claimed)
	}
	lines, _, stop := f.stream("alice-token")
	defer stop()
	if !waitFor(lines, "connected", 2*time.Second) {
		t.Fatal("no stream greeting")
	}
	f.srv.checkChanged() // the state clients see now, as the periodic refresh would have recorded
	ck.add(3 * time.Hour)
	if c, _, _ := f.do("POST", itemPath(id, "claim"), "alice-token"); c != 200 {
		t.Errorf("claiming a lapsed item = %d, want 200", c)
	}
	if !waitFor(lines, "event: change", 2*time.Second) {
		t.Error("subscriber was not told that the item changed hands")
	}
}

func TestLease_PeriodicCheckNoticesALapse(t *testing.T) {
	f, ck := leasedFixture(t)
	id := firstItemID(t, f)
	f.do("POST", itemPath(id, "claim"), "bob-token")
	lines, _, stop := f.stream("alice-token")
	defer stop()
	waitFor(lines, "connected", 2*time.Second)
	f.srv.checkChanged()
	ck.add(3 * time.Hour)
	if err := f.srv.Refresh(); err != nil { // what the refresh timer does
		t.Fatal(err)
	}
	if !waitFor(lines, "event: change", 2*time.Second) {
		t.Error("a lapse was not pushed to subscribers by the periodic check")
	}
	if _, it, _ := f.do("GET", "/items/"+url.PathEscape(id), "alice-token"); it["holder"] != nil {
		t.Errorf("lapsed holder still shown: %v", it)
	}
}

func TestConfigClaimLease(t *testing.T) {
	dir := t.TempDir()
	write := func(extra string) string {
		p := dir + "/c.json"
		_ = os.WriteFile(p, []byte(`{`+extra+`"repos":["/x"],"members":[{"identity":"a","token":"t","admin":true}]}`), 0o644)
		return p
	}
	c, err := LoadConfig(write(""))
	if err != nil || c.ClaimLeaseDuration() != 8*time.Hour {
		t.Errorf("default lease = %v %v, want 8h", c.ClaimLeaseDuration(), err)
	}
	if c, _ = LoadConfig(write(`"claim_lease":"0s",`)); c.ClaimLeaseDuration() != 0 {
		t.Errorf("0s lease = %v", c.ClaimLeaseDuration())
	}
	if _, err := LoadConfig(write(`"claim_lease":"soon",`)); err == nil {
		t.Error("garbage lease accepted")
	}
	if !c.Members[0].Admin {
		t.Error("admin flag not read")
	}
}

func TestConfigGitCredentials(t *testing.T) {
	dir := t.TempDir()
	write := func(extra string) (Config, error) {
		p := dir + "/c.json"
		_ = os.WriteFile(p, []byte(`{`+extra+`"members":[{"identity":"a","token":"t"}]}`), 0o644)
		return LoadConfig(p)
	}
	t.Setenv("MY_GH_TOKEN", "ghp_abc")
	t.Setenv("EMPTY_TOKEN", "  ")
	c, err := write(`"repos":["https://github.com/o/priv.git"],"git_credentials":[{"host":"github.com","token_env":"MY_GH_TOKEN"}],`)
	if err != nil {
		t.Fatal(err)
	}
	if g, _ := c.GitAuth(); len(g.Credentials) != 1 || g.Credentials[0].Token != "ghp_abc" {
		t.Errorf("token not read from the environment: %+v", g)
	}
	if _, err := write(`"repos":["x"],"git_credentials":[{"host":"github.com","token_env":"EMPTY_TOKEN"}],`); err == nil || !strings.Contains(err.Error(), "EMPTY_TOKEN") {
		t.Errorf("empty token variable: %v, want an error naming it", err)
	}
	if _, err := write(`"repos":["x"],"git_credentials":[{"host":"github.com"}],`); err == nil {
		t.Error("credential without token_env accepted")
	}
	for _, bad := range []string{"https://user:pw@github.com/o/r.git", "https://ghp_abc@github.com/o/r.git"} {
		if _, err := write(`"repos":["` + bad + `"],`); err == nil || !strings.Contains(err.Error(), "git_credentials") || strings.Contains(err.Error(), "ghp_abc") || strings.Contains(err.Error(), "pw@") {
			t.Errorf("URL with credentials %q: %v, want a refusal that points to git_credentials without echoing the secret", bad, err)
		}
	}
	if _, err := write(`"repos":["ssh://git@host/o/r.git","git@github.com:o/r.git"],`); err != nil {
		t.Errorf("ssh URLs with the ssh user must be fine: %v", err)
	}
}
