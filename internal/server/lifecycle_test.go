package server

import (
	"bufio"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBearerSchemeIsCaseInsensitive(t *testing.T) {
	f := newFixture(t)
	for _, scheme := range []string{"Bearer", "bearer", "BEARER"} {
		req, _ := http.NewRequest("GET", f.http.URL+"/me", nil)
		req.Header.Set("Authorization", scheme+" alice-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("scheme %q = %d, want 200", scheme, resp.StatusCode)
		}
	}
	req, _ := http.NewRequest("GET", f.http.URL+"/me", nil)
	req.Header.Set("Authorization", "bearer ALICE-TOKEN") // the token stays case-sensitive
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Errorf("wrong-case token = %d, want 401", resp.StatusCode)
	}
}

func TestLoadingServerAnswers503ButHealthzStaysUp(t *testing.T) {
	f := newFixture(t)
	f.srv.SetLoading()
	if code, obj, _ := f.do("GET", "/healthz", ""); code != 200 || obj["ready"] != false {
		t.Errorf("healthz while loading = %d %v, want 200 and ready=false", code, obj)
	}
	if code, obj, _ := f.do("GET", "/items", "alice-token"); code != 503 || obj["error"] == nil {
		t.Errorf("items while loading = %d %v, want 503", code, obj)
	}
	if code, _, _ := f.do("GET", "/items", "wrong"); code != 401 {
		t.Errorf("a bad token must still be 401 while loading, got %d", code)
	}
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	if code, obj, _ := f.do("GET", "/healthz", ""); code != 200 || obj["ready"] != true {
		t.Errorf("healthz after the first read = %d %v", code, obj)
	}
	if code, _, _ := f.do("GET", "/items", "alice-token"); code != 200 {
		t.Errorf("items after the first read = %d, want 200", code)
	}
}

func TestCloseStreamsEndsOpenEventStreams(t *testing.T) {
	f := newFixture(t)
	req, _ := http.NewRequest("GET", f.http.URL+"/events", nil)
	req.Header.Set("Authorization", "Bearer alice-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ended := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
		}
		close(ended)
	}()
	time.Sleep(100 * time.Millisecond)
	f.srv.CloseStreams()
	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the stream was still open 2 s after CloseStreams: a graceful shutdown would wait for it")
	}
}

func TestTwoReposWithTheSameIDDoNotShareItems(t *testing.T) {
	f := newFixture(t)
	second := t.TempDir()
	_ = os.MkdirAll(filepath.Join(second, ".beads"), 0o755)
	_ = os.WriteFile(filepath.Join(second, "relai.yaml"), []byte("id: billing-api\nsummary: A different repo, same id.\n"), 0o644)
	if out, err := exec.Command("git", "-C", second, "init", "-q", "-b", "main").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	f.stop()
	f.cfg.Repos = append(f.cfg.Repos, second)
	f.start()

	code, _, repos := f.do("GET", "/repos", "alice-token")
	if code != 200 || len(repos) != 2 {
		t.Fatalf("repos = %d %v", code, repos)
	}
	first, dup := repos[0].(map[string]any), repos[1].(map[string]any)
	if first["readable"] != true {
		t.Errorf("the first repo with the id should stay readable: %v", first)
	}
	if dup["readable"] != false || !strings.Contains(dup["error"].(string), "already used") || !strings.Contains(dup["error"].(string), "billing-api") {
		t.Errorf("the second repo must say why it is not used: %v", dup)
	}
	_, _, items := f.do("GET", "/items", "alice-token")
	if len(items) != 2 { // the two fixture tasks, once
		t.Errorf("items = %d, want 2 (no duplicates)", len(items))
	}
}
