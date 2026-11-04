package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func mirrorConfig(t *testing.T, forge string, extra string) string {
	t.Helper()
	t.Setenv("FORGE_TOKEN_FOR_TEST", "ghp_verysecrettoken")
	p := filepath.Join(t.TempDir(), "c.json")
	body := `{"members":[{"identity":"alice","token":"alice-token-0123456789"}],"issue_mirrors":[{"provider":"github","project":"acme/app","token_env":"FORGE_TOKEN_FOR_TEST","base_url":"` + forge + `"` + extra + `}]}`
	_ = os.WriteFile(p, []byte(body), 0o644)
	return p
}

func TestIssueMirrorConfigIsValidated(t *testing.T) {
	good := mirrorConfig(t, "https://api.github.com", "")
	if cfg, err := LoadConfig(good); err != nil || len(cfg.IssueMirrors) != 1 {
		t.Fatalf("a valid mirror-only config was refused: %v", err)
	}
	for name, c := range map[string]struct{ forge, extra, want string }{
		"plain http off loopback": {"http://forge.example.com", "", "https"},
		"credentials in the URL":  {"https://user:pw@api.github.com", "", "credentials"},
		"bad interval":            {"https://api.github.com", `,"interval":"5s"`, "at least 1m"},
		"bad repo id":             {"https://api.github.com", `,"repo_id":"a b"`, "repo_id"},
		"bad provider":            {"https://api.github.com", `,"provider":"bitbucket"`, "provider"},
	} {
		p := mirrorConfig(t, c.forge, c.extra)
		if c.want == "provider" { // the second "provider" key would be a duplicate: rewrite instead
			b, _ := os.ReadFile(p)
			_ = os.WriteFile(p, []byte(strings.Replace(string(b), `"provider":"github"`, `"provider":"bitbucket"`, 1)), 0o644)
		}
		if _, err := LoadConfig(p); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one mentioning %q", name, err, c.want)
		}
	}
	// the variable must be set, and its value must never show in the error
	t.Setenv("FORGE_TOKEN_FOR_TEST", "")
	if _, err := LoadConfig(good); err == nil || !strings.Contains(err.Error(), "FORGE_TOKEN_FOR_TEST") {
		t.Errorf("an empty token variable must be named in the error: %v", err)
	}
	// loopback http is allowed (a local fake forge), a second identical mirror is not
	t.Setenv("FORGE_TOKEN_FOR_TEST", "tok")
	if _, err := LoadConfig(mirrorConfig(t, "http://127.0.0.1:9", "")); err != nil {
		t.Errorf("loopback http refused: %v", err)
	}
}

func TestIssuesAppearAsClaimableItemsAndFailuresKeepThem(t *testing.T) {
	var down atomic.Bool
	var gotAuth atomic.Value
	forge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		if down.Load() {
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, `[{"number":1,"title":"First issue","labels":[]},{"number":2,"title":"Second","labels":[{"name":"blocked"}]}]`)
	}))
	defer forge.Close()

	f := newFixture(t)
	f.stop()
	f.cfg.IssueMirrors = []IssueMirrorConfig{{Provider: "github", Project: "acme/app", TokenEnv: "FORGE_TOKEN_FOR_TEST", BaseURL: forge.URL}}
	t.Setenv("FORGE_TOKEN_FOR_TEST", "ghp_verysecrettoken")
	f.start()

	_, _, items := f.do("GET", "/items?repo=app", "alice-token")
	if len(items) != 2 {
		t.Fatalf("mirrored items = %v", items)
	}
	if gotAuth.Load() != "Bearer ghp_verysecrettoken" {
		t.Errorf("the forge was not sent the token: %v", gotAuth.Load())
	}
	// claim an issue: it is held in the team's journal, nothing is written to the forge (the fake has no write route)
	if code, obj, _ := f.do("POST", "/items/app%2Fgh%2F1/claim", "alice-token"); code != 200 || obj["holder"] != "alice" {
		t.Errorf("claim = %d %v", code, obj)
	}
	_, _, repos := f.do("GET", "/repos", "alice-token")
	found := false
	for _, r := range repos {
		if m := r.(map[string]any); m["source"] == "github:acme/app" && m["readable"] == true && m["items"].(float64) == 2 {
			found = true
		}
	}
	if !found {
		t.Errorf("the mirror is not listed as a readable repo with its items: %v", repos)
	}

	// the forge goes down: items and the claim stay, the repo is reported unreadable with a reason and no token
	down.Store(true)
	f.srv.mirrors[0].Now = nil
	f.srv.mirrors[0].Src.Interval = 1 // ask again at once
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	_, _, items = f.do("GET", "/items?repo=app", "alice-token")
	if len(items) != 2 {
		t.Errorf("a forge outage wiped the mirrored items: %v", items)
	}
	_, _, repos = f.do("GET", "/repos", "alice-token")
	for _, r := range repos {
		m := r.(map[string]any)
		if m["source"] == "github:acme/app" {
			if m["readable"] != false || strings.Contains(fmt.Sprint(m["error"]), "ghp_verysecrettoken") {
				t.Errorf("outage not reported cleanly: %v", m)
			}
		}
	}
}
