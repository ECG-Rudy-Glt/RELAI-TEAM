package mirror

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const ghIssues = `[
 {"number":7,"title":"Fix login","labels":[{"name":"bug"},{"name":"in progress"}]},
 {"number":8,"title":"A pull request","pull_request":{"url":"x"},"labels":[]},
 {"number":9,"title":"Waiting on legal\u001b[31m","labels":[{"name":"Blocked"}]},
 {"number":10,"title":"Plain one","labels":[]}
]`

func gh(t *testing.T, h http.HandlerFunc) (*Mirror, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	m := New(Source{Provider: "github", Project: "acme/app", BaseURL: srv.URL, RepoID: "app", Token: "SECRET-TOKEN-123"})
	return m, srv
}

func TestGitHubIssuesBecomeItems(t *testing.T) {
	var gotAuth, gotPath string
	m, _ := gh(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.RequestURI()
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, ghIssues)
	})
	repo, err := m.Read()
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer SECRET-TOKEN-123" || !strings.Contains(gotPath, "/repos/acme/app/issues") || !strings.Contains(gotPath, "sort=updated") {
		t.Errorf("request = %q %q", gotAuth, gotPath)
	}
	if repo.ID != "app" || repo.Source != "github:acme/app" || len(repo.Items) != 3 {
		t.Fatalf("repo = %+v (the pull request must not be an item)", repo)
	}
	byID := map[string]string{}
	for _, it := range repo.Items {
		byID[it.ID] = it.Status
		if strings.ContainsAny(it.Title, "\x1b") {
			t.Errorf("control character kept: %q", it.Title)
		}
	}
	if byID["app/gh/7"] != "in_progress" || byID["app/gh/9"] != "blocked" || byID["app/gh/10"] != "open" {
		t.Errorf("statuses = %v", byID)
	}
}

func TestGitLabIssuesUseTheIIDAndTheTokenHeader(t *testing.T) {
	var gotToken, gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken, gotURL = r.Header.Get("PRIVATE-TOKEN"), r.URL.EscapedPath()
		fmt.Fprint(w, `[{"iid":3,"title":"Ship it","labels":["status::doing"]},{"iid":4,"title":"Later","labels":[]}]`)
	}))
	defer srv.Close()
	m := New(Source{Provider: "gitlab", Project: "group/sub/app", BaseURL: srv.URL, RepoID: "app", Token: "glpat-xyz"})
	repo, err := m.Read()
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != "glpat-xyz" || !strings.Contains(gotURL, "/api/v4/projects/group%2Fsub%2Fapp/issues") {
		t.Errorf("token %q url %q", gotToken, gotURL)
	}
	if len(repo.Items) != 2 || repo.Items[0].ID != "app/gl/3" || repo.Items[0].Status != "in_progress" {
		t.Errorf("items = %+v", repo.Items)
	}
}

func TestUnchangedAnswerCostsNothingAndIsNotRepeatedWithinTheInterval(t *testing.T) {
	var full, notModified atomic.Int32
	m, _ := gh(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			notModified.Add(1)
			w.WriteHeader(304)
			return
		}
		full.Add(1)
		w.Header().Set("ETag", `"v1"`)
		fmt.Fprint(w, ghIssues)
	})
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return now }
	m.Src.Interval = time.Minute
	first, _ := m.Read()
	if _, err := m.Read(); err != nil { // same instant: no request at all
		t.Fatal(err)
	}
	if m.Requests() != 1 {
		t.Errorf("a second read within the interval made a request: %d", m.Requests())
	}
	now = now.Add(2 * time.Minute)
	again, err := m.Read()
	if err != nil || len(again.Items) != len(first.Items) || notModified.Load() != 1 || full.Load() != 1 {
		t.Errorf("after the interval: err=%v items=%d full=%d 304=%d", err, len(again.Items), full.Load(), notModified.Load())
	}
	// past FullRefresh the whole list is read again whatever the ETag says (closed issues do not change page 1)
	now = now.Add(7 * time.Hour)
	if _, err := m.Read(); err != nil || full.Load() != 2 {
		t.Errorf("safety re-read missing: err=%v full=%d", err, full.Load())
	}
}

func TestPaginationStopsAtTheCap(t *testing.T) {
	var srvURL string
	m, srv := gh(t, func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		if page == "" {
			page = "1"
		}
		var n int
		fmt.Sscan(page, &n)
		var b strings.Builder
		b.WriteString("[")
		for i := 0; i < 100; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"number":%d,"title":"t","labels":[]}`, n*1000+i)
		}
		b.WriteString("]")
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/app/issues?page=%d>; rel="next"`, srvURL, n+1))
		fmt.Fprint(w, b.String())
	})
	srvURL = srv.URL
	repo, err := m.Read()
	if err != nil || len(repo.Items) != MaxIssues || len(repo.Notes) != 1 || m.Requests() != 3 {
		t.Errorf("err=%v items=%d notes=%v requests=%d, want %d items, a note, 3 requests", err, len(repo.Items), repo.Notes, m.Requests(), MaxIssues)
	}
}

func TestRateLimitMakesTheMirrorStopAsking(t *testing.T) {
	m, _ := gh(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(403)
	})
	now := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	m.Now = func() time.Time { return now }
	if _, err := m.Read(); err == nil || !strings.Contains(err.Error(), "rate limited") {
		t.Fatalf("err = %v", err)
	}
	now = now.Add(5 * time.Minute)
	if _, err := m.Read(); err == nil || m.Requests() != 1 {
		t.Errorf("asked again during the back-off: requests=%d err=%v", m.Requests(), err)
	}
	now = now.Add(6 * time.Minute)
	m.Read()
	if m.Requests() != 2 {
		t.Errorf("did not resume after the back-off: %d", m.Requests())
	}
}

func TestFailuresSayWhyAndNeverShowTheToken(t *testing.T) {
	for name, code := range map[string]int{"unauthorized": 401, "missing": 404, "boom": 500} {
		m, _ := gh(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code) })
		_, err := m.Read()
		if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN-123") {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	m := New(Source{Provider: "github", Project: "a/b", BaseURL: "http://127.0.0.1:1", RepoID: "b", Token: "SECRET-TOKEN-123"})
	if _, err := m.Read(); err == nil || strings.Contains(err.Error(), "SECRET-TOKEN-123") {
		t.Errorf("unreachable: %v", err)
	}
	bad, _ := gh(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "<html>not json</html>") })
	if _, err := bad.Read(); err == nil || !strings.Contains(err.Error(), "not a list of issues") {
		t.Errorf("garbage answer: %v", err)
	}
}

func TestARedirectToAnotherHostIsRefused(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
		fmt.Fprint(w, "[]")
	}))
	defer other.Close()
	m, _ := gh(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL+"/steal", 302) })
	// 127.0.0.1:A and 127.0.0.1:B are different hosts for this purpose
	if _, err := m.Read(); err == nil || leaked.Load() {
		t.Errorf("redirect followed: err=%v leaked=%v", err, leaked.Load())
	}
}
