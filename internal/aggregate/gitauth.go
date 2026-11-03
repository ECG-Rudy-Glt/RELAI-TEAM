package aggregate

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// GitCredential is an access token for HTTPS remotes on one host.
type GitCredential struct {
	Host     string // "github.com", or "host:port"
	Username string // sent with the token; "x-access-token" works for GitHub, "oauth2" for GitLab
	Token    string
	// TokenEnv is the name of the environment variable the token came from, for error messages.
	TokenEnv string
}

// GitAuth is how the server authenticates to private remotes. The zero value means "no
// credentials": git then still never waits for a person, it fails with a reason.
type GitAuth struct {
	Credentials []GitCredential
	SSHKey      string // private key file for ssh:// and git@ remotes
	KnownHosts  string // known_hosts file to verify ssh host keys against; empty = accept first seen
}

// EnvRunner runs a command with extra environment variables. It is the only way credentials reach
// git: through the environment, never through the command line or the remote URL.
type EnvRunner func(dir string, env []string, name string, args ...string) ([]byte, error)

// ExecRunnerEnv is ExecRunner with extra environment; git is always run without terminal prompts.
func ExecRunnerEnv(dir string, env []string, name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	if name == "git" {
		cmd.Env = append(cmd.Env, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never")
	}
	type result struct {
		out []byte
		err error
	}
	done := make(chan result, 1)
	go func() { out, err := cmd.Output(); done <- result{out, err} }()
	select {
	case r := <-done:
		return r.out, r.err
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		return nil, fmt.Errorf("%s timed out", name)
	}
}

type remote struct {
	scheme string // http, https, ssh, git, file
	host   string // as in the URL, with port
	name   string // host without port
}

var scpLike = regexp.MustCompile(`^[\w.-]+@([\w.-]+):`)

func parseRemote(src string) (remote, bool) {
	if m := scpLike.FindStringSubmatch(src); m != nil && !strings.Contains(src, "://") {
		return remote{scheme: "ssh", host: m[1], name: m[1]}, true
	}
	u, err := url.Parse(src)
	if err != nil || u.Scheme == "" {
		return remote{}, false
	}
	return remote{scheme: u.Scheme, host: u.Host, name: u.Hostname()}, true
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// env returns the environment to run network git against src. problem is not empty when a
// credential is configured for the host but must not be used (plain HTTP).
func (g GitAuth) env(src string) (env []string, problem string) {
	r, ok := parseRemote(src)
	if !ok {
		return nil, ""
	}
	switch r.scheme {
	case "ssh":
		cmd := "ssh -o BatchMode=yes"
		if g.SSHKey != "" {
			cmd += " -i " + shellQuote(g.SSHKey) + " -o IdentitiesOnly=yes"
		}
		if g.KnownHosts != "" {
			cmd += " -o UserKnownHostsFile=" + shellQuote(g.KnownHosts) + " -o StrictHostKeyChecking=yes"
		} else {
			cmd += " -o StrictHostKeyChecking=accept-new"
		}
		return []string{"GIT_SSH_COMMAND=" + cmd}, ""
	case "http", "https":
		for _, c := range g.Credentials {
			if !strings.EqualFold(c.Host, r.host) && !strings.EqualFold(c.Host, r.name) {
				continue
			}
			if r.scheme == "http" {
				return nil, fmt.Sprintf("a credential is configured for %s but the remote is plain http://, so it was not sent: use an https:// URL", r.name)
			}
			user := c.Username
			if user == "" {
				user = "x-access-token"
			}
			header := "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+c.Token))
			// Scoped to this exact scheme and host, so a redirect elsewhere never receives it.
			return []string{
				"GIT_CONFIG_COUNT=1",
				"GIT_CONFIG_KEY_0=http." + r.scheme + "://" + r.host + "/.extraHeader",
				"GIT_CONFIG_VALUE_0=" + header,
			}, ""
		}
	}
	return nil, ""
}

// scrub removes every known secret from s.
func (g GitAuth) scrub(s string) string {
	for _, c := range g.Credentials {
		if c.Token == "" {
			continue
		}
		user := c.Username
		if user == "" {
			user = "x-access-token"
		}
		s = strings.ReplaceAll(s, base64.StdEncoding.EncodeToString([]byte(user+":"+c.Token)), "***")
		s = strings.ReplaceAll(s, c.Token, "***")
	}
	return s
}

// explain turns a failed git call into a short, secret-free reason that says what to do.
func (g GitAuth) explain(src string, err error) error {
	var stderr string
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		stderr = string(ee.Stderr)
	}
	host := src
	var credEnv string
	hasCred := false
	if r, ok := parseRemote(src); ok {
		host = r.name
		for _, c := range g.Credentials {
			if strings.EqualFold(c.Host, r.host) || strings.EqualFold(c.Host, r.name) {
				hasCred, credEnv = true, c.TokenEnv
			}
		}
	}
	low := strings.ToLower(stderr)
	asksForCredentials := strings.Contains(low, "terminal prompts disabled") || strings.Contains(low, "could not read username") || strings.Contains(low, "could not read password")
	switch {
	case asksForCredentials && !hasCred:
		return fmt.Errorf("authentication required for %s: no credential configured (add it to git_credentials)", host)
	case asksForCredentials, strings.Contains(low, "authentication failed"), strings.Contains(low, "returned error: 401"), strings.Contains(low, "returned error: 403"):
		// with a credential configured, being asked again means the remote rejected it
		hint := "check the token"
		if credEnv != "" {
			hint = "check the token in $" + credEnv
		}
		return fmt.Errorf("authentication refused by %s: %s", host, hint)
	case strings.Contains(low, "host key verification failed"):
		return fmt.Errorf("ssh host key of %s is not trusted: add it to the file named by git_known_hosts", host)
	case strings.Contains(low, "permission denied (publickey"):
		return fmt.Errorf("ssh key refused by %s: check git_ssh_key", host)
	case strings.Contains(low, "repository not found"), strings.Contains(low, "returned error: 404"):
		return fmt.Errorf("repository not found on %s (or the credential cannot see it)", host)
	}
	first := ""
	for _, l := range strings.Split(g.scrub(stderr), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			first = l
			break
		}
	}
	if first == "" {
		first = g.scrub(err.Error())
	}
	if len(first) > 160 {
		first = first[:160] + "…"
	}
	return fmt.Errorf("%s", first)
}

// runNet runs a git command that talks to src's remote, with src's credentials in the environment.
func (a *Aggregator) runNet(src string, args ...string) ([]byte, error) {
	env, problem := a.Git.env(src)
	if problem != "" {
		return nil, errors.New(problem)
	}
	var out []byte
	var err error
	if a.RunEnv != nil {
		out, err = a.RunEnv("", env, "git", args...)
	} else {
		out, err = a.Run("", "git", args...)
	}
	if err != nil {
		return out, a.Git.explain(src, err)
	}
	return out, nil
}
