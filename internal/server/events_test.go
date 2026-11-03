package server

import (
	"bufio"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// stream opens /events and returns a channel of its lines plus a stop function.
func (f *fixture) stream(token string) (lines <-chan string, status int, stop func()) {
	f.t.Helper()
	req, _ := http.NewRequest("GET", f.http.URL+"/events", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		f.t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, resp.StatusCode, func() {}
	}
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			ch <- sc.Text()
		}
	}()
	return ch, 200, func() { resp.Body.Close() }
}

func waitFor(ch <-chan string, want string, d time.Duration) bool {
	timeout := time.After(d)
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				return false
			}
			if strings.Contains(l, want) {
				return true
			}
		case <-timeout:
			return false
		}
	}
}

func TestBoardPageIsPublicAndHoldsNoData(t *testing.T) {
	f := newFixture(t)
	resp, err := http.Get(f.http.URL + "/board")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(strings.Builder)
	_, _ = bufio.NewReader(resp.Body).WriteTo(buf)
	body := buf.String()
	if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET /board = %d %s", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
	for _, leak := range []string{"billing-api", "Wire the PDF endpoint", "Document it", "alice", "bob"} {
		if strings.Contains(body, leak) {
			t.Errorf("the public page contains board/member data %q", leak)
		}
	}
	if csp := resp.Header.Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self'") || !strings.Contains(csp, "frame-ancestors 'none'") {
		t.Errorf("CSP = %q", csp)
	}
	if code, _, _ := f.do("GET", "/items", ""); code != 401 {
		t.Errorf("/items without a token = %d, want 401", code)
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r2, err := noFollow.Get(f.http.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	r2.Body.Close()
	if r2.StatusCode != http.StatusSeeOther || r2.Header.Get("Location") != "/board" {
		t.Errorf("GET / = %d -> %q, want redirect to /board", r2.StatusCode, r2.Header.Get("Location"))
	}
}

func TestEventsNeedAToken(t *testing.T) {
	f := newFixture(t)
	if _, code, _ := f.stream(""); code != 401 {
		t.Errorf("subscribing without a token = %d, want 401", code)
	}
	if _, code, _ := f.stream("nope"); code != 401 {
		t.Errorf("subscribing with a wrong token = %d, want 401", code)
	}
}

func TestClaimNotifiesSubscribers(t *testing.T) {
	f := newFixture(t)
	lines, code, stop := f.stream("bob-token")
	if code != 200 {
		t.Fatalf("subscribe = %d", code)
	}
	defer stop()
	if !waitFor(lines, "connected", 2*time.Second) {
		t.Fatal("no stream greeting")
	}
	id := firstItemID(t, f)
	if c, _, _ := f.do("POST", itemPath(id, "claim"), "alice-token"); c != 200 {
		t.Fatalf("claim = %d", c)
	}
	if !waitFor(lines, "event: change", 2*time.Second) {
		t.Error("bob was not told that alice claimed an item")
	}
}

func TestUnchangedRefreshSendsNothingChangedRefreshDoes(t *testing.T) {
	f := newFixture(t)
	lines, _, stop := f.stream("alice-token")
	defer stop()
	if !waitFor(lines, "connected", 2*time.Second) {
		t.Fatal("no stream greeting")
	}
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	if waitFor(lines, "event: change", 400*time.Millisecond) {
		t.Error("a refresh that found nothing new still notified subscribers")
	}
	f.setBD(`[{"id":"b-1","title":"Wire the PDF endpoint","status":"in_progress","issue_type":"task"}]`)
	if err := f.srv.Refresh(); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, "event: change", 2*time.Second) {
		t.Error("a changed task (and a closed one) did not notify subscribers")
	}
}

func TestEventsCarryNoBoardData(t *testing.T) {
	f := newFixture(t)
	lines, _, stop := f.stream("alice-token")
	defer stop()
	waitFor(lines, "connected", 2*time.Second)
	f.do("POST", itemPath(firstItemID(t, f), "claim"), "alice-token")
	var got []string
	deadline := time.After(1 * time.Second)
loop:
	for {
		select {
		case l := <-lines:
			got = append(got, l)
		case <-deadline:
			break loop
		}
	}
	all := strings.Join(got, "\n")
	for _, leak := range []string{"billing-api", "Wire the PDF", "alice"} {
		if strings.Contains(all, leak) {
			t.Errorf("event stream leaks %q: %s", leak, all)
		}
	}
}

func firstItemID(t *testing.T, f *fixture) string {
	t.Helper()
	_, _, list := f.do("GET", "/items", "alice-token")
	if len(list) == 0 {
		t.Fatal("no items")
	}
	return list[0].(map[string]any)["id"].(string)
}

func TestConfigTLSAndLoopback(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1:8080": false, "localhost:8080": false, "[::1]:8080": false,
		":8080": true, "0.0.0.0:8080": true, "10.1.2.3:8080": true, "myhost.example:8080": true, "garbage": true,
	} {
		if got := ListensBeyondLoopback(addr); got != want {
			t.Errorf("ListensBeyondLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
	dir := t.TempDir()
	write := func(body string) string {
		p := dir + "/c.json"
		if err := writeFile(p, body); err != nil {
			t.Fatal(err)
		}
		return p
	}
	base := `"repos":["/x"],"members":[{"identity":"a","token":"t"}]`
	if _, err := LoadConfig(write(`{"tls_cert":"c.pem",` + base + `}`)); err == nil || !strings.Contains(err.Error(), "tls_key is missing") {
		t.Errorf("cert without key: %v", err)
	}
	if _, err := LoadConfig(write(`{"tls_key":"k.pem",` + base + `}`)); err == nil || !strings.Contains(err.Error(), "tls_cert is missing") {
		t.Errorf("key without cert: %v", err)
	}
	if _, err := LoadConfig(write(`{"refresh_concurrency":-1,` + base + `}`)); err == nil {
		t.Error("negative refresh_concurrency accepted")
	}
	c, err := LoadConfig(write(`{"tls_cert":"c.pem","tls_key":"k.pem",` + base + `}`))
	if err != nil || !c.UsesTLS() {
		t.Errorf("both set: %v uses=%v", err, c.UsesTLS())
	}
}

func writeFile(p, body string) error { return os.WriteFile(p, []byte(body), 0o644) }
