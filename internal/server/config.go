package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/mirror"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Config is the server's configuration file (JSON).
type Config struct {
	Listen          string   `json:"listen"`           // default ":8080"
	DataDir         string   `json:"data_dir"`         // default "data"; holds events.jsonl
	RefreshInterval string   `json:"refresh_interval"` // default "30s"
	Repos           []string `json:"repos"`            // local paths or clone URLs
	Members         []Member `json:"members"`
	// GitCredentials, GitSSHKey and GitKnownHosts let repos declared by clone URL be private.
	GitCredentials []GitCredentialConfig `json:"git_credentials"`
	GitSSHKey      string                `json:"git_ssh_key"`
	GitKnownHosts  string                `json:"git_known_hosts"`
	// ClaimLease is how long a claim lasts unless its holder renews it by claiming again
	// (default "8h"). "0s" means claims never lapse.
	ClaimLease string `json:"claim_lease"`
	// FullRefreshInterval is the longest an unchanged repo is trusted before being read again
	// anyway (default "6h"). Repos are otherwise only read when their files change, because each
	// read runs bd, which rewrites its database. "0s" reads every repo every cycle (expensive).
	FullRefreshInterval string `json:"full_refresh_interval"`
	// RefreshConcurrency caps how many repos are read at once; default 8.
	RefreshConcurrency int `json:"refresh_concurrency"`
	// Discover lists folders whose git repositories are added to the board automatically (re-scanned at each
	// refresh, so a project cloned there appears without editing this file). Read-only.
	Discover []DiscoverConfig `json:"discover"`
	// IssueMirrors reflects the open issues of GitHub or GitLab projects onto the board, read-only.
	IssueMirrors []IssueMirrorConfig `json:"issue_mirrors"`
	// TLSCert and TLSKey, both or neither, make the server speak HTTPS.
	TLSCert string `json:"tls_cert"`
	TLSKey  string `json:"tls_key"`
}

// GitCredentialConfig lets the server read a private repo over HTTPS. The token itself never goes
// in the config file: TokenEnv names the environment variable that holds it.
type GitCredentialConfig struct {
	Host     string `json:"host"`               // "github.com" (or "host:port")
	Username string `json:"username,omitempty"` // default "x-access-token"; GitLab wants "oauth2"
	TokenEnv string `json:"token_env"`
}

// IssueMirrorConfig is one GitHub or GitLab project whose open issues appear as work items. The token
// itself never goes in the config file: TokenEnv names the environment variable that holds it, and a
// read-only token (issues: read) is enough, the server never writes to the forge.
type IssueMirrorConfig struct {
	Provider string `json:"provider"`           // "github" or "gitlab"
	Project  string `json:"project"`            // "owner/name", or "group/sub/name" on GitLab
	TokenEnv string `json:"token_env"`          // environment variable holding the token
	BaseURL  string `json:"base_url,omitempty"` // https API root; default api.github.com / gitlab.com (self-hosted GitLab: https://gitlab.example.com)
	RepoID   string `json:"repo_id,omitempty"`  // id the items appear under; default: the project's last path segment
	Interval string `json:"interval,omitempty"` // shortest time between requests to the forge; default 5m, at least 1m
}

// DiscoverConfig is a folder to scan for repositories.
type DiscoverConfig struct {
	Folder string `json:"folder"`
	Depth  int    `json:"depth,omitempty"` // folder levels to look down; default 1
}

var repoIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// Mirrors validates the issue mirrors and builds them, reading each token from its environment
// variable. An empty variable is an error at startup, not an unreadable project later.
func (c Config) Mirrors() ([]*mirror.Mirror, error) {
	var out []*mirror.Mirror
	seen, ids := map[string]bool{}, map[string]bool{}
	for i, mc := range c.IssueMirrors {
		where := fmt.Sprintf("issue_mirrors[%d]", i)
		if mc.Provider != "github" && mc.Provider != "gitlab" {
			return nil, fmt.Errorf("%s: provider must be \"github\" or \"gitlab\"", where)
		}
		mc.Project = strings.Trim(strings.TrimSpace(mc.Project), "/")
		if mc.Project == "" || !strings.Contains(mc.Project, "/") || strings.ContainsAny(mc.Project, " ?#\\") || strings.Contains(mc.Project, "..") {
			return nil, fmt.Errorf("%s: project must look like owner/name", where)
		}
		if mc.TokenEnv == "" {
			return nil, fmt.Errorf("%s: token_env is required (the name of the environment variable holding a read-only token)", where)
		}
		tok := strings.TrimSpace(os.Getenv(mc.TokenEnv))
		if tok == "" {
			return nil, fmt.Errorf("%s (%s): environment variable %s is empty or not set", where, mc.Project, mc.TokenEnv)
		}
		base := mc.BaseURL
		if base == "" {
			base = map[string]string{"github": "https://api.github.com", "gitlab": "https://gitlab.com"}[mc.Provider]
		}
		u, err := url.Parse(base)
		if err != nil || u.Host == "" || u.User != nil {
			return nil, fmt.Errorf("%s: base_url is not a usable URL (credentials in it are refused)", where)
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && !ListensBeyondLoopback(net.JoinHostPort(u.Hostname(), "0"))) {
			return nil, fmt.Errorf("%s: base_url must be https: the token would travel in clear", where)
		}
		id := mc.RepoID
		if id == "" {
			id = mc.Project[strings.LastIndex(mc.Project, "/")+1:]
		}
		if !repoIDPattern.MatchString(id) {
			return nil, fmt.Errorf("%s: repo_id %q must be letters, digits, '.', '_' or '-'", where, id)
		}
		key := mc.Provider + ":" + mc.Project
		if seen[key] {
			return nil, fmt.Errorf("%s: %s is declared twice", where, key)
		}
		if ids[id] {
			return nil, fmt.Errorf("%s: repo_id %q is used by another mirror: set repo_id to tell them apart", where, id)
		}
		seen[key], ids[id] = true, true
		var interval time.Duration
		if mc.Interval != "" {
			d, err := time.ParseDuration(mc.Interval)
			if err != nil || d < time.Minute {
				return nil, fmt.Errorf("%s: interval must be a duration of at least 1m, such as 5m", where)
			}
			interval = d
		}
		out = append(out, mirror.New(mirror.Source{Provider: mc.Provider, Project: mc.Project, BaseURL: base, RepoID: id, Token: tok, Interval: interval}))
	}
	return out, nil
}

// Member is one identity of the declared team. Give either the token itself or its SHA-256 (hex),
// so the config file need not hold plaintext secrets.
type Member struct {
	Identity    string `json:"identity"`
	Token       string `json:"token,omitempty"`
	TokenSHA256 string `json:"token_sha256,omitempty"`
	// Admin lets this member force-release items held by others (explicitly, with ?force=true).
	Admin bool `json:"admin,omitempty"`
}

// LoadConfig reads and validates a config file, filling defaults.
func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	// Unknown fields are refused: a typo such as "claim_leas" would otherwise be ignored and the
	// default silently used.
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if err := checkRepos(path, c.Repos, len(c.IssueMirrors) > 0, len(c.Discover) > 0); err != nil {
		return Config{}, err
	}
	if c.Listen == "" {
		c.Listen = ":8080"
	}
	if c.DataDir == "" {
		c.DataDir = "data"
	}
	if c.RefreshInterval == "" {
		c.RefreshInterval = "30s"
	}
	if (c.TLSCert == "") != (c.TLSKey == "") {
		missing := "tls_key"
		if c.TLSCert == "" {
			missing = "tls_cert"
		}
		return Config{}, fmt.Errorf("%s: tls_cert and tls_key go together: %s is missing", path, missing)
	}
	for _, src := range c.Repos {
		if u, err := url.Parse(src); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.User != nil {
			return Config{}, fmt.Errorf("%s: repo %q contains credentials in its URL, which would be shown to every member: remove them and use git_credentials", path, u.Host)
		}
	}
	if _, err := c.GitAuth(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	for i, d := range c.Discover {
		if strings.TrimSpace(d.Folder) == "" || d.Depth < 0 || d.Depth > 5 {
			return Config{}, fmt.Errorf("%s: discover[%d]: folder is required and depth is 1 to 5", path, i)
		}
	}
	if _, err := c.Mirrors(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	if c.ClaimLease == "" {
		c.ClaimLease = "8h"
	}
	if d, err := time.ParseDuration(c.ClaimLease); err != nil || d < 0 || (d > 0 && d < time.Second) {
		return Config{}, fmt.Errorf("%s: claim_lease must be a duration of at least 1s like 8h, or 0s for claims that never lapse", path)
	}
	if c.FullRefreshInterval == "" {
		c.FullRefreshInterval = "6h"
	}
	if d, err := time.ParseDuration(c.FullRefreshInterval); err != nil || d < 0 {
		return Config{}, fmt.Errorf("%s: full_refresh_interval must be a duration like 6h, or 0s to always read", path)
	}
	if c.RefreshConcurrency < 0 {
		return Config{}, fmt.Errorf("%s: refresh_concurrency must not be negative", path)
	}
	if _, err := c.Interval(); err != nil {
		return Config{}, fmt.Errorf("%s: refresh_interval: %w", path, err)
	}
	if _, err := c.tokens(); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
	}
	// Checks for a config file written by hand; the server itself accepts any identity and token
	// (tests build configs in code).
	for i, m := range c.Members {
		if !identityPattern.MatchString(m.Identity) {
			return Config{}, fmt.Errorf("%s: members[%d]: identity %q must be 1-64 letters, digits, '.', '_', '-' or '@' (it appears in the log and the web page)", path, i, m.Identity)
		}
		if m.Token != "" && len(m.Token) < minTokenLength {
			return Config{}, fmt.Errorf("%s: members[%d] (%s): token is shorter than %d characters; use `relai-server init` to generate one", path, i, m.Identity, minTokenLength)
		}
	}
	return c, nil
}

// checkRepos refuses an empty list (a server with nothing to show), blank entries, and the same
// repository declared twice, directly or through a symbolic link.
func checkRepos(path string, repos []string, hasMirrors, hasDiscover bool) error {
	if len(repos) == 0 && !hasMirrors && !hasDiscover {
		return fmt.Errorf("%s: repos: declare at least one repository (a local path or a clone URL) an issue_mirrors entry or a discover folder", path)
	}
	seen := map[string]int{}
	for i, r := range repos {
		if strings.TrimSpace(r) == "" {
			return fmt.Errorf("%s: repos[%d] is empty", path, i)
		}
		key := r
		if !strings.Contains(r, "://") && !strings.HasPrefix(r, "git@") {
			key = filepath.Clean(r)
			if real, err := filepath.EvalSymlinks(key); err == nil {
				key = real
			}
		}
		if j, dup := seen[key]; dup {
			return fmt.Errorf("%s: repos[%d] (%s) is the same repository as repos[%d]: declare it once", path, i, r, j)
		}
		seen[key] = i
	}
	return nil
}

// minTokenLength is the shortest plain token accepted. Generated tokens are 48 hex characters;
// anything much shorter can be guessed.
const minTokenLength = 16

var identityPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{0,63}$`)

// GitAuth resolves the git credentials, reading each token from its environment variable. An
// empty variable is an error here, at startup, rather than an unreadable repo later.
func (c Config) GitAuth() (aggregate.GitAuth, error) {
	g := aggregate.GitAuth{SSHKey: c.GitSSHKey, KnownHosts: c.GitKnownHosts}
	for _, p := range []string{c.GitSSHKey, c.GitKnownHosts} {
		if strings.ContainsAny(p, "'\n") {
			return g, errors.New("git_ssh_key and git_known_hosts must not contain quotes or newlines")
		}
	}
	for i, gc := range c.GitCredentials {
		if gc.Host == "" || gc.TokenEnv == "" {
			return g, fmt.Errorf("git_credentials[%d]: host and token_env are required", i)
		}
		tok := strings.TrimSpace(os.Getenv(gc.TokenEnv))
		if tok == "" {
			return g, fmt.Errorf("git_credentials[%d] (%s): environment variable %s is empty or not set", i, gc.Host, gc.TokenEnv)
		}
		g.Credentials = append(g.Credentials, aggregate.GitCredential{Host: gc.Host, Username: gc.Username, Token: tok, TokenEnv: gc.TokenEnv})
	}
	return g, nil
}

// ClaimLeaseDuration parses ClaimLease (0, also for an unset value, means claims never lapse).
func (c Config) ClaimLeaseDuration() time.Duration {
	d, _ := time.ParseDuration(c.ClaimLease)
	return d
}

// FullRefresh parses FullRefreshInterval (0 means always read).
func (c Config) FullRefresh() time.Duration {
	d, _ := time.ParseDuration(c.FullRefreshInterval)
	return d
}

// UsesTLS reports whether the server should serve HTTPS.
func (c Config) UsesTLS() bool { return c.TLSCert != "" && c.TLSKey != "" }

// ListensBeyondLoopback reports whether addr accepts connections from other machines (an empty
// host, a wildcard, or any non-loopback address). An unparseable address counts as beyond.
func ListensBeyondLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return true
	}
	if host == "" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsLoopback()
	}
	return host != "localhost"
}

// Interval parses RefreshInterval.
func (c Config) Interval() (time.Duration, error) {
	d, err := time.ParseDuration(c.RefreshInterval)
	if err != nil {
		return 0, err
	}
	if d < time.Second {
		return 0, errors.New("must be at least 1s")
	}
	return d, nil
}

// tokens maps the SHA-256 of each member's token to their identity.
func (c Config) tokens() (map[[32]byte]string, error) {
	if len(c.Members) == 0 {
		return nil, errors.New("members: at least one member is required")
	}
	out := map[[32]byte]string{}
	seen := map[string]bool{}
	for i, m := range c.Members {
		if m.Identity == "" {
			return nil, fmt.Errorf("members[%d]: identity is required", i)
		}
		if seen[m.Identity] {
			return nil, fmt.Errorf("members[%d]: duplicate identity %q", i, m.Identity)
		}
		seen[m.Identity] = true
		var h [32]byte
		switch {
		case m.Token != "" && m.TokenSHA256 != "":
			return nil, fmt.Errorf("members[%d] (%s): give token or token_sha256, not both", i, m.Identity)
		case m.Token != "":
			h = sha256.Sum256([]byte(m.Token))
		case m.TokenSHA256 != "":
			raw, err := hex.DecodeString(m.TokenSHA256)
			if err != nil || len(raw) != 32 {
				return nil, fmt.Errorf("members[%d] (%s): token_sha256 must be 64 hex characters", i, m.Identity)
			}
			copy(h[:], raw)
		default:
			return nil, fmt.Errorf("members[%d] (%s): token or token_sha256 is required", i, m.Identity)
		}
		if _, dup := out[h]; dup {
			return nil, fmt.Errorf("members[%d] (%s): token already used by another member", i, m.Identity)
		}
		out[h] = m.Identity
	}
	return out, nil
}
