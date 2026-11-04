// Package initcfg builds a ready-to-run relai-server configuration: it finds the repos worth
// tracking in a folder, creates one random token per member, and stores only the tokens' hashes.
package initcfg

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Options says what to put in the configuration.
type Options struct {
	Repos   []string // repos to track (paths or clone URLs)
	Members []string // identities; the first one is the administrator
	Listen  string   // default ":8080"
	DataDir string   // where the event log goes
	Rand    io.Reader
}

// Member is a created member with the token to hand to them (shown once, never stored).
type Member struct {
	Identity string
	Token    string
	Admin    bool
}

// Result is the configuration to write and the tokens to show.
type Result struct {
	JSON    []byte
	Members []Member
}

var identity = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Generate builds the configuration. Tokens are random and only their SHA-256 goes in the file.
func Generate(o Options) (Result, error) {
	if len(o.Repos) == 0 {
		return Result{}, errors.New("no repos to track: pass --repos a,b or --scan <folder>")
	}
	for _, r := range o.Repos {
		isURL := strings.Contains(r, "://") || strings.HasPrefix(r, "git@")
		if st, err := os.Stat(r); !isURL && (err != nil || !st.IsDir()) {
			return Result{}, fmt.Errorf("repo %q is not a directory on this machine: check the path (clone URLs are fine too)", r)
		}
	}
	if len(o.Members) == 0 {
		return Result{}, errors.New("at least one member is required")
	}
	if o.Rand == nil {
		o.Rand = rand.Reader
	}
	if o.Listen == "" {
		o.Listen = ":8080"
	}
	type cfgMember struct {
		Identity    string `json:"identity"`
		TokenSHA256 string `json:"token_sha256"`
		Admin       bool   `json:"admin,omitempty"`
	}
	var res Result
	var cm []cfgMember
	seen := map[string]bool{}
	for i, id := range o.Members {
		id = strings.TrimSpace(id)
		if !identity.MatchString(id) {
			return Result{}, fmt.Errorf("member %q: use letters, digits, '.', '_' or '-'", id)
		}
		if seen[id] {
			return Result{}, fmt.Errorf("member %q listed twice", id)
		}
		seen[id] = true
		raw := make([]byte, 24)
		if _, err := io.ReadFull(o.Rand, raw); err != nil {
			return Result{}, err
		}
		tok := "rt_" + hex.EncodeToString(raw)
		sum := sha256.Sum256([]byte(tok))
		res.Members = append(res.Members, Member{Identity: id, Token: tok, Admin: i == 0})
		cm = append(cm, cfgMember{Identity: id, TokenSHA256: hex.EncodeToString(sum[:]), Admin: i == 0})
	}
	cfg := map[string]any{
		"listen":           o.Listen,
		"data_dir":         o.DataDir,
		"refresh_interval": "30s",
		"repos":            o.Repos,
		"members":          cm,
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Result{}, err
	}
	res.JSON = append(b, '\n')
	return res, nil
}

// Scan returns the git repos directly inside dir (or dir itself) that carry something the board can
// show: a .beads folder, an openspec folder or a relai.yaml.
func Scan(dir string) ([]string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	candidates := []string{abs}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			candidates = append(candidates, filepath.Join(abs, e.Name()))
		}
	}
	var out []string
	for _, c := range candidates {
		if !exists(filepath.Join(c, ".git")) {
			continue
		}
		if exists(filepath.Join(c, ".beads")) || exists(filepath.Join(c, "openspec")) || exists(filepath.Join(c, "relai.yaml")) {
			out = append(out, c)
		}
	}
	sort.Strings(out)
	return out, nil
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

// Rotate gives a member a new token. The new token's hash is added to the member's "token_sha256s", so
// the old token keeps working until the new one has been handed out; with dropOld the old credentials are
// removed at once (use it for the second step, or when a token leaked). The config is read as generic JSON so
// nothing else in it is touched, and written atomically with mode 0600. It returns the new token, which
// is shown once and never stored.
func Rotate(path, identity string, dropOld bool, r io.Reader) (string, error) {
	if r == nil {
		r = rand.Reader
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	members, _ := cfg["members"].([]any)
	var target map[string]any
	for _, m := range members {
		if mm, ok := m.(map[string]any); ok && mm["identity"] == identity {
			target = mm
		}
	}
	if target == nil {
		return "", fmt.Errorf("no member %q in %s", identity, path)
	}
	raw := make([]byte, 24)
	if _, err := io.ReadFull(r, raw); err != nil {
		return "", err
	}
	tok := "rt_" + hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(tok))
	h := hex.EncodeToString(sum[:])
	if dropOld {
		delete(target, "token")
		delete(target, "token_sha256s")
		target["token_sha256"] = h
	} else {
		list, _ := target["token_sha256s"].([]any)
		target["token_sha256s"] = append(list, h)
	}
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	return tok, nil
}
