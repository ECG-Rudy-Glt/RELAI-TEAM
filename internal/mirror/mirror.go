// Package mirror reflects the open issues of a GitHub or GitLab project onto the board as work items,
// read-only: the server never writes to the forge. A claim stays in the team's own journal.
//
// Polling is the dangerous part, so it is bounded: a conditional request (ETag) costs nothing when
// nothing changed, the first page is sorted by last update so any edit changes it, the whole list is
// re-read only every FullRefresh as a safety net, and a rate limit makes the mirror stop asking until it
// is over. The token is read from an environment variable, sent only to the configured host, and never
// appears in an error, a log line or an API answer.
package mirror

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/aggregate"
)

const (
	// MaxIssues is how many open issues are mirrored (the most recently updated ones).
	MaxIssues = 300
	perPage   = 100
	maxBody   = 8 << 20
	// DefaultInterval is the shortest time between two requests to the forge.
	DefaultInterval = 5 * time.Minute
	// DefaultFullRefresh is how long an unchanged answer is trusted before the whole list is read again.
	DefaultFullRefresh = 6 * time.Hour
)

// Source describes one mirrored project.
type Source struct {
	Provider string // "github" or "gitlab"
	Project  string // "owner/name" (GitHub) or "group/sub/name" (GitLab)
	BaseURL  string // API root; defaults to api.github.com / gitlab.com
	RepoID   string // the repo id its items appear under
	Token    string // from the environment; never logged
	Interval time.Duration
}

// Name is how the source is shown to clients, e.g. "github:owner/name".
func (s Source) Name() string { return s.Provider + ":" + s.Project }

// Mirror reads one Source, politely.
type Mirror struct {
	Src         Source
	HTTP        *http.Client
	FullRefresh time.Duration
	Now         func() time.Time // for tests

	mu          sync.Mutex
	etag        string
	repo        aggregate.Repo
	have        bool
	lastRequest time.Time
	lastFull    time.Time
	blockedTill time.Time
	requests    int
	failures    int       // consecutive failures
	retryAt     time.Time // no request before this, after a failure
	lastErr     error
}

// New returns a mirror for src. The HTTP client never follows a redirect to another host, so the token
// cannot be sent anywhere else.
func New(src Source) *Mirror {
	m := &Mirror{Src: src, FullRefresh: DefaultFullRefresh}
	m.HTTP = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Host != via[0].URL.Host {
			return errors.New("redirect to another host refused")
		}
		if len(via) > 3 {
			return errors.New("too many redirects")
		}
		return nil
	}}
	return m
}

// Requests is how many HTTP requests have been made; for tests and logs.
func (m *Mirror) Requests() int { m.mu.Lock(); defer m.mu.Unlock(); return m.requests }

func (m *Mirror) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

// Read returns the mirrored project as a Repo. Within Interval of the last request it returns the last
// answer without asking again. On any failure the error says why, without the token.
func (m *Mirror) Read() (aggregate.Repo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	// After a failure, wait before asking again: the interval, doubling at each consecutive failure up to
	// an hour. Without this a source that never succeeded would be asked at every refresh.
	if now.Before(m.retryAt) {
		return aggregate.Repo{}, m.lastErr
	}
	r, err := m.read(now)
	if err != nil && m.blockedTill.After(now) {
		return aggregate.Repo{}, err // a rate limit has its own, exact, wait: blockedTill
	}
	if err != nil {
		m.failures++
		wait := m.interval()
		for i := 1; i < m.failures && wait < time.Hour; i++ {
			wait *= 2
		}
		if wait > time.Hour {
			wait = time.Hour
		}
		m.retryAt, m.lastErr = now.Add(wait), err
		return aggregate.Repo{}, err
	}
	m.failures, m.retryAt, m.lastErr = 0, time.Time{}, nil
	return r, nil
}

func (m *Mirror) interval() time.Duration {
	if m.Src.Interval > 0 {
		return m.Src.Interval
	}
	return DefaultInterval
}

func (m *Mirror) read(now time.Time) (aggregate.Repo, error) {
	if now.Before(m.blockedTill) {
		return aggregate.Repo{}, fmt.Errorf("rate limited by %s until %s", m.Src.Provider, m.blockedTill.UTC().Format("15:04Z"))
	}
	if m.have && now.Sub(m.lastRequest) < m.interval() {
		return m.repo, nil
	}
	m.lastRequest = now
	useETag := m.have && m.etag != "" && now.Sub(m.lastFull) < m.FullRefresh

	var all []aggregate.Item
	next := m.firstURL()
	truncated := false
	for page := 1; next != ""; page++ {
		resp, body, err := m.get(next, page == 1 && useETag)
		if err != nil {
			return aggregate.Repo{}, err
		}
		if resp.StatusCode == http.StatusNotModified {
			return m.repo, nil
		}
		items, err := m.parse(body)
		if err != nil {
			return aggregate.Repo{}, err
		}
		if page == 1 {
			m.etag = resp.Header.Get("ETag")
		}
		all = append(all, items...)
		next = nextLink(resp, m.Src.Provider)
		if len(all) >= MaxIssues && next != "" {
			all, truncated = all[:MaxIssues], true
			break
		}
	}
	r := aggregate.Repo{Source: m.Src.Name(), ID: m.Src.RepoID, Summary: "Open issues of " + m.Src.Project + " (read-only mirror)", Items: all}
	if truncated {
		r.Notes = append(r.Notes, fmt.Sprintf("only the %d most recently updated open issues are shown", MaxIssues))
	}
	m.repo, m.have, m.lastFull = r, true, now
	return r, nil
}

func (m *Mirror) firstURL() string {
	base := strings.TrimRight(m.Src.BaseURL, "/")
	if m.Src.Provider == "gitlab" {
		return fmt.Sprintf("%s/api/v4/projects/%s/issues?state=opened&order_by=updated_at&sort=desc&per_page=%d", base, url.PathEscape(m.Src.Project), perPage)
	}
	return fmt.Sprintf("%s/repos/%s/issues?state=open&sort=updated&direction=desc&per_page=%d", base, m.Src.Project, perPage)
}

func (m *Mirror) get(u string, conditional bool) (*http.Response, []byte, error) {
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, nil, errors.New("invalid issue source URL")
	}
	if m.Src.Provider == "gitlab" {
		req.Header.Set("PRIVATE-TOKEN", m.Src.Token)
	} else {
		req.Header.Set("Authorization", "Bearer "+m.Src.Token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	}
	req.Header.Set("User-Agent", "relai-server")
	if conditional {
		req.Header.Set("If-None-Match", m.etag)
	}
	m.requests++
	resp, err := m.HTTP.Do(req)
	if err != nil {
		// url.Error repeats the URL (no secret in it); never the headers.
		return nil, nil, fmt.Errorf("%s unreachable: %v", m.Src.Provider, scrub(err, m.Src.Token))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return resp, nil, nil
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, nil, fmt.Errorf("%s refused the token (401): check the token and its read permission for issues", m.Src.Provider)
	case resp.StatusCode == http.StatusNotFound:
		return nil, nil, fmt.Errorf("%s says %s does not exist, or the token cannot see it (404)", m.Src.Provider, m.Src.Project)
	case resp.StatusCode == http.StatusTooManyRequests || (resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0"):
		wait := retryAfter(resp, m.now())
		m.blockedTill = m.now().Add(wait)
		return nil, nil, fmt.Errorf("rate limited by %s (%d): not asking again for %s", m.Src.Provider, resp.StatusCode, wait.Round(time.Second))
	case resp.StatusCode/100 != 2:
		return nil, nil, fmt.Errorf("%s answered HTTP %d", m.Src.Provider, resp.StatusCode)
	}
	return resp, body, nil
}

func scrub(err error, token string) string {
	s := err.Error()
	if token != "" {
		s = strings.ReplaceAll(s, token, "***")
	}
	return s
}

// retryAfter is how long to stay quiet after a rate-limit answer: the server's own hint, else 15 minutes.
func retryAfter(resp *http.Response, now time.Time) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			if d := time.Unix(n, 0).Sub(now); d > 0 {
				return d + time.Second
			}
		}
	}
	return 15 * time.Minute
}

// nextLink returns the next page from the Link header (both forges send one), only on the same host.
func nextLink(resp *http.Response, provider string) string {
	for _, part := range strings.Split(resp.Header.Get("Link"), ",") {
		if strings.Contains(part, `rel="next"`) {
			if i, j := strings.Index(part, "<"), strings.Index(part, ">"); i >= 0 && j > i {
				u := part[i+1 : j]
				if nu, err := url.Parse(u); err == nil && resp.Request != nil && nu.Host == resp.Request.URL.Host {
					return u
				}
			}
		}
	}
	return ""
}

func (m *Mirror) parse(body []byte) ([]aggregate.Item, error) {
	var out []aggregate.Item
	if m.Src.Provider == "gitlab" {
		var raw []struct {
			IID    int64    `json:"iid"`
			Title  string   `json:"title"`
			Labels []string `json:"labels"`
		}
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, errors.New("gitlab returned something that is not a list of issues")
		}
		for _, r := range raw {
			out = append(out, m.item("gl", r.IID, r.Title, r.Labels))
		}
		return out, nil
	}
	var raw []struct {
		Number      int64           `json:"number"`
		Title       string          `json:"title"`
		PullRequest json.RawMessage `json:"pull_request"`
		Labels      []struct {
			Name string `json:"name"`
		} `json:"labels"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, errors.New("github returned something that is not a list of issues")
	}
	for _, r := range raw {
		if len(r.PullRequest) > 0 { // GitHub lists pull requests as issues
			continue
		}
		var labels []string
		for _, l := range r.Labels {
			labels = append(labels, l.Name)
		}
		out = append(out, m.item("gh", r.Number, r.Title, labels))
	}
	return out, nil
}

func (m *Mirror) item(prefix string, n int64, title string, labels []string) aggregate.Item {
	return aggregate.Item{
		ID: fmt.Sprintf("%s/%s/%d", m.Src.RepoID, prefix, n), Repo: m.Src.RepoID, Kind: "issue",
		Title: cleanTitle(title), Status: statusFromLabels(labels),
	}
}

// statusFromLabels maps the labels people actually use to the board's statuses. Anything else is open.
func statusFromLabels(labels []string) string {
	status := "open"
	for _, l := range labels {
		switch strings.ToLower(strings.TrimSpace(l)) {
		case "blocked", "status::blocked", "status: blocked":
			return "blocked"
		case "in progress", "in-progress", "in_progress", "doing", "wip", "status::in progress", "status::doing", "status: in progress":
			status = "in_progress"
		}
	}
	return status
}

// cleanTitle drops control characters and bounds the length: the title comes from a stranger on the internet.
func cleanTitle(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == 0x2028 || r == 0x2029 {
			return -1
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 200 {
		s = string(r[:199]) + "…"
	}
	return s
}
