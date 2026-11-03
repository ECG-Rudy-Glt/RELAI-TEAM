package aggregate

import (
	"crypto/subtle"
	"encoding/pem"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const secretToken = "ghp_SUPERSECRET_token_123"

// privateRemote serves one repo ("priv.git") through the real `git http-backend` over TLS, behind
// HTTP Basic auth like GitHub: user "x-access-token", password = the token.
type privateRemote struct {
	srv       *httptest.Server
	noAuthHdr atomic.Int32 // requests that arrived with no Authorization header
	anyAuth   atomic.Int32
	url       string
	host      string
}

func newPrivateRemote(t *testing.T, tls bool) *privateRemote {
	t.Helper()
	backend := filepath.Join(gitExecPath(t), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("git http-backend not available")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	g := func(dir string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	g(work, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(work, "relai.yaml"), []byte("id: private-svc\nsummary: Only for the team.\n"), 0o644)
	g(work, "add", "-A")
	g(work, "commit", "-q", "-m", "init")
	g(root, "clone", "-q", "--bare", work, filepath.Join(root, "priv.git"))

	pr := &privateRemote{}
	cgiH := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok {
			pr.noAuthHdr.Add(1)
		} else {
			pr.anyAuth.Add(1)
		}
		if !ok || user != "x-access-token" || subtle.ConstantTimeCompare([]byte(pass), []byte(secretToken)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="git"`)
			http.Error(w, "Authentication failed", http.StatusUnauthorized)
			return
		}
		cgiH.ServeHTTP(w, r)
	})
	if tls {
		pr.srv = httptest.NewTLSServer(h)
		ca := filepath.Join(t.TempDir(), "ca.pem")
		_ = os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pr.srv.Certificate().Raw}), 0o644)
		t.Setenv("GIT_SSL_CAINFO", ca) // so real git trusts the test server's certificate
	} else {
		pr.srv = httptest.NewServer(h)
	}
	t.Cleanup(pr.srv.Close)
	pr.url = pr.srv.URL + "/priv.git"
	pr.host = strings.TrimPrefix(strings.TrimPrefix(pr.srv.URL, "https://"), "http://")
	return pr
}

func gitExecPath(t *testing.T) string {
	out, err := exec.Command("git", "--exec-path").Output()
	if err != nil {
		t.Skip("no git")
	}
	return strings.TrimSpace(string(out))
}

func realAggregator(auth GitAuth) *Aggregator {
	a := New()
	a.Git = auth
	return a
}

func TestPrivate_ReadWithConfiguredToken(t *testing.T) {
	r := newPrivateRemote(t, true)
	a := realAggregator(GitAuth{Credentials: []GitCredential{{Host: r.host, Token: secretToken, TokenEnv: "GITHUB_TOKEN"}}})
	repos, skipped := a.All([]string{r.url})
	if len(skipped) != 0 || len(repos) != 1 || repos[0].ID != "private-svc" {
		t.Fatalf("repos=%v skipped=%v", repos, skipped)
	}
}

func TestPrivate_NoCredentialFailsFastWithAHelpfulReason(t *testing.T) {
	r := newPrivateRemote(t, true)
	a := realAggregator(GitAuth{})
	start := time.Now()
	_, skipped := a.All([]string{r.url})
	if took := time.Since(start); took > 15*time.Second {
		t.Errorf("took %v: git must never wait for a person to type a credential", took)
	}
	if len(skipped) != 1 {
		t.Fatalf("skipped = %v", skipped)
	}
	reason := skipped[0].Reason
	if !strings.Contains(reason, "authentication required") || !strings.Contains(reason, "git_credentials") {
		t.Errorf("reason = %q, want it to say a credential is needed and where to set it", reason)
	}
	if strings.Contains(reason, "exit status") {
		t.Errorf("reason is just %q: not helpful", reason)
	}
}

func TestPrivate_WrongTokenIsRefusedAndNeverEchoed(t *testing.T) {
	r := newPrivateRemote(t, true)
	wrong := "ghp_WRONG_token_999"
	a := realAggregator(GitAuth{Credentials: []GitCredential{{Host: r.host, Token: wrong, TokenEnv: "GITHUB_TOKEN"}}})
	_, skipped := a.All([]string{r.url})
	if len(skipped) != 1 {
		t.Fatalf("skipped = %v", skipped)
	}
	reason := skipped[0].Reason
	if !strings.Contains(reason, "authentication refused") || !strings.Contains(reason, "$GITHUB_TOKEN") {
		t.Errorf("reason = %q", reason)
	}
	for _, secret := range []string{wrong, secretToken, "Basic ", "x-access-token:"} {
		if strings.Contains(reason, secret) {
			t.Errorf("the reported reason leaks %q: %q", secret, reason)
		}
	}
}

func TestPrivate_CredentialIsNotSentOverPlainHTTP(t *testing.T) {
	r := newPrivateRemote(t, false)
	a := realAggregator(GitAuth{Credentials: []GitCredential{{Host: r.host, Token: secretToken, TokenEnv: "GITHUB_TOKEN"}}})
	_, skipped := a.All([]string{r.url})
	if len(skipped) != 1 || !strings.Contains(skipped[0].Reason, "https") {
		t.Fatalf("skipped = %v, want a reason saying https is required", skipped)
	}
	if n := r.anyAuth.Load(); n != 0 {
		t.Errorf("%d request(s) carried credentials over plain http", n)
	}
	if r.noAuthHdr.Load() != 0 {
		t.Errorf("the server was contacted anyway (%d requests): nothing should be attempted", r.noAuthHdr.Load())
	}
}

func TestEnv_NeverPutsTheTokenInURLsOrArgs(t *testing.T) {
	g := GitAuth{Credentials: []GitCredential{{Host: "github.com", Username: "oauth2", Token: "tok123", TokenEnv: "T"}}}
	env, problem := g.env("https://github.com/org/priv.git")
	if problem != "" || len(env) != 3 {
		t.Fatalf("env = %v problem = %q", env, problem)
	}
	if !strings.HasPrefix(env[1], "GIT_CONFIG_KEY_0=http.https://github.com/.extraHeader") {
		t.Errorf("header must be scoped to the exact host: %v", env)
	}
	if !strings.Contains(env[2], "Authorization: Basic ") {
		t.Errorf("header = %q", env[2])
	}
	if e, _ := g.env("https://gitlab.com/org/x.git"); len(e) != 0 {
		t.Errorf("a credential for github.com leaked to gitlab.com: %v", e)
	}
	if e, _ := g.env("file:///tmp/x"); len(e) != 0 {
		t.Errorf("file remote got an environment: %v", e)
	}
	if got := g.scrub("fatal: bad tok123 and " + "Basic b2F1dGgyOnRvazEyMw=="); strings.Contains(got, "tok123") || strings.Contains(got, "b2F1dGgyOnRvazEyMw==") {
		t.Errorf("scrub left a secret: %q", got)
	}
}

func TestEnv_SSH(t *testing.T) {
	for _, src := range []string{"git@github.com:org/priv.git", "ssh://git@host.example/org/priv.git"} {
		env, _ := GitAuth{SSHKey: "/run/secrets/id", KnownHosts: "/run/secrets/kh"}.env(src)
		if len(env) != 1 || !strings.Contains(env[0], "-i '/run/secrets/id'") || !strings.Contains(env[0], "BatchMode=yes") ||
			!strings.Contains(env[0], "StrictHostKeyChecking=yes") || !strings.Contains(env[0], "UserKnownHostsFile='/run/secrets/kh'") {
			t.Errorf("%s: env = %v", src, env)
		}
	}
	env, _ := GitAuth{}.env("git@github.com:org/priv.git")
	if len(env) != 1 || !strings.Contains(env[0], "BatchMode=yes") || !strings.Contains(env[0], "accept-new") {
		t.Errorf("ssh with no key must still never prompt: %v", env)
	}
}
