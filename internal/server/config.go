package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

// Config is the server's configuration file (JSON).
type Config struct {
	Listen          string   `json:"listen"`           // default ":8080"
	DataDir         string   `json:"data_dir"`         // default "data"; holds events.jsonl
	RefreshInterval string   `json:"refresh_interval"` // default "30s"
	Repos           []string `json:"repos"`            // local paths or clone URLs
	Members         []Member `json:"members"`
	// FullRefreshInterval is the longest an unchanged repo is trusted before being read again
	// anyway (default "6h"). Repos are otherwise only read when their files change, because each
	// read runs bd, which rewrites its database. "0s" reads every repo every cycle (expensive).
	FullRefreshInterval string `json:"full_refresh_interval"`
	// RefreshConcurrency caps how many repos are read at once; default 8.
	RefreshConcurrency int `json:"refresh_concurrency"`
	// TLSCert and TLSKey, both or neither, make the server speak HTTPS.
	TLSCert string `json:"tls_cert"`
	TLSKey  string `json:"tls_key"`
}

// Member is one identity of the declared team. Give either the token itself or its SHA-256 (hex),
// so the config file need not hold plaintext secrets.
type Member struct {
	Identity    string `json:"identity"`
	Token       string `json:"token,omitempty"`
	TokenSHA256 string `json:"token_sha256,omitempty"`
}

// LoadConfig reads and validates a config file, filling defaults.
func LoadConfig(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("%s: %w", path, err)
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
	return c, nil
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
