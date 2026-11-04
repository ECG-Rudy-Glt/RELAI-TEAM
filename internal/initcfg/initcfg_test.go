package initcfg

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/server"
)

func TestGenerate_ConfigLoadsAndOnlyHashesAreStored(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	res, err := Generate(Options{Repos: []string{a, b}, Members: []string{"alice", "agent-1"}, DataDir: "/d"})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, res.JSON, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := server.LoadConfig(p)
	if err != nil {
		t.Fatalf("the generated config does not load: %v\n%s", err, res.JSON)
	}
	if len(cfg.Repos) != 2 || len(cfg.Members) != 2 || cfg.Listen != ":8080" {
		t.Errorf("cfg = %+v", cfg)
	}
	if !cfg.Members[0].Admin || cfg.Members[1].Admin {
		t.Errorf("the first member should be the only admin: %+v", cfg.Members)
	}
	for _, m := range res.Members {
		if strings.Contains(string(res.JSON), m.Token) {
			t.Errorf("the config file contains %s's plaintext token", m.Identity)
		}
		sum := sha256.Sum256([]byte(m.Token))
		if !strings.Contains(string(res.JSON), hex.EncodeToString(sum[:])) {
			t.Errorf("the hash of %s's token is not in the config", m.Identity)
		}
		if !strings.HasPrefix(m.Token, "rt_") || len(m.Token) != 3+48 {
			t.Errorf("token format: %q", m.Token)
		}
	}
	if res.Members[0].Token == res.Members[1].Token {
		t.Error("two members got the same token")
	}
}

func TestGenerate_TokensAreRandomAcrossRuns(t *testing.T) {
	a, _ := Generate(Options{Repos: []string{os.TempDir()}, Members: []string{"x"}})
	b, _ := Generate(Options{Repos: []string{os.TempDir()}, Members: []string{"x"}})
	if a.Members[0].Token == b.Members[0].Token {
		t.Error("tokens repeat between runs")
	}
}

func TestGenerate_Validation(t *testing.T) {
	for name, o := range map[string]Options{
		"no repos":          {Members: []string{"a"}},
		"no members":        {Repos: []string{os.TempDir()}},
		"bad identity":      {Repos: []string{os.TempDir()}, Members: []string{"a b"}},
		"duplicate":         {Repos: []string{os.TempDir()}, Members: []string{"a", "a"}},
		"quote in identity": {Repos: []string{os.TempDir()}, Members: []string{`a"b`}},
	} {
		if _, err := Generate(o); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := Generate(Options{Repos: []string{os.TempDir()}, Members: []string{"a"}, Rand: bytes.NewReader(nil)}); err == nil {
		t.Error("a failing random source must be an error, not a weak token")
	}
}

func TestScan_FindsOnlyRepoWithSomethingToShow(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string, dirs ...string) {
		for _, d := range append(dirs, ".git") {
			if err := os.MkdirAll(filepath.Join(root, rel, d), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("with-beads", ".beads")
	mk("with-openspec", "openspec")
	mk("with-yaml")
	_ = os.WriteFile(filepath.Join(root, "with-yaml", "relai.yaml"), []byte("id: x\n"), 0o644)
	mk("plain-git")                                                  // a git repo with nothing the board shows
	_ = os.MkdirAll(filepath.Join(root, "not-git", ".beads"), 0o755) // not a git repo
	mk(".hidden", ".beads")
	got, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, g := range got {
		names = append(names, filepath.Base(g))
	}
	if strings.Join(names, ",") != "with-beads,with-openspec,with-yaml" {
		t.Errorf("scan = %v, want with-beads, with-openspec, with-yaml", names)
	}
	if _, err := Scan(filepath.Join(root, "missing")); err == nil {
		t.Error("scanning a missing folder should fail")
	}
}

func TestGenerate_RefusesARepoThatDoesNotExist(t *testing.T) {
	if _, err := Generate(Options{Repos: []string{"/nonexistent/a"}, Members: []string{"x"}}); err == nil {
		t.Error("a missing local path was written into the config")
	}
	if _, err := Generate(Options{Repos: []string{"https://example.com/a.git"}, Members: []string{"x"}}); err != nil {
		t.Errorf("a clone URL must be accepted: %v", err)
	}
}
