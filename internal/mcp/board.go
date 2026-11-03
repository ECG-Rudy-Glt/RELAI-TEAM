package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// Item mirrors the team server's work item.
type Item struct {
	ID     string `json:"id"`
	Repo   string `json:"repo"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Holder string `json:"holder,omitempty"`
}

// Client talks to a relai-server as one team member. It decides nothing: every rule (identity,
// atomic claims, visibility) is the server's.
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client

	meOnce sync.Once
	me     string
}

// who returns the caller's own identity ("" if the server cannot say), asked once. Agents are
// not told their identity name, so holder fields need a "(you)" marker to be readable.
func (c *Client) who() string {
	c.meOnce.Do(func() {
		var m struct{ Identity string }
		if c.do("GET", "/me", &m) == nil {
			c.me = m.Identity
		}
	})
	return c.me
}

// apiError is a non-2xx answer from the server.
type apiError struct {
	Status int
	Msg    string
	Holder string
}

func (e *apiError) Error() string { return e.Msg }

func (c *Client) do(method, path string, out any) error {
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequest(method, strings.TrimRight(c.BaseURL, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("relai-server unreachable at %s: %v", c.BaseURL, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		var e struct{ Error, Holder string }
		_ = json.Unmarshal(body, &e)
		msg := e.Error
		switch {
		case resp.StatusCode == http.StatusUnauthorized:
			msg = "authentication failed: RELAI_TEAM_TOKEN is not a declared team member"
		case msg == "":
			msg = fmt.Sprintf("unexpected answer from relai-server: HTTP %d", resp.StatusCode)
		}
		return &apiError{Status: resp.StatusCode, Msg: msg, Holder: e.Holder}
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("unexpected answer from relai-server: %v", err)
		}
	}
	return nil
}

func (c *Client) items() ([]Item, error) {
	var it []Item
	err := c.do("GET", "/items", &it)
	return it, err
}

func (c *Client) claim(id string) (Item, error) {
	var it Item
	err := c.do("POST", "/items/"+url.PathEscape(id)+"/claim", &it)
	return it, err
}

func (c *Client) release(id string) (Item, error) {
	var it Item
	err := c.do("POST", "/items/"+url.PathEscape(id)+"/release", &it)
	return it, err
}

func (c *Client) line(i Item) string {
	h := "-"
	if i.Holder != "" {
		h = i.Holder
		if i.Holder == c.who() {
			h += " (you)"
		}
	}
	return fmt.Sprintf("%s [%s] holder=%s  %s", i.ID, i.Status, h, i.Title)
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

const instructions = "This is the team board: work items from several repos, each held by at most one " +
	"person or agent. Before you start a task, claim it (board_claim or board_claim_next); if the " +
	"claim is refused, someone else is on it, so pick other work. Release what you will not finish."

// Instructions is sent to the client at initialize time.
func Instructions() string { return instructions }

// Tools returns the board tools bound to c.
func Tools(c *Client) []Tool {
	fail := func(err error) (string, bool) {
		if ae, ok := err.(*apiError); ok && ae.Holder != "" && !strings.Contains(ae.Msg, ae.Holder) {
			return fmt.Sprintf("%s (held by %s)", ae.Msg, ae.Holder), true
		}
		return err.Error(), true
	}
	return []Tool{
		{
			Name:        "board_repos",
			Description: "List the repos the team board tracks, with how many open work items each has and whether it could be read.",
			InputSchema: obj(map[string]any{}),
			Handler: func(json.RawMessage) (string, bool) {
				var repos []struct {
					ID       string `json:"id"`
					Source   string `json:"source"`
					Readable bool   `json:"readable"`
					Items    int    `json:"items"`
					Error    string `json:"error"`
				}
				if err := c.do("GET", "/repos", &repos); err != nil {
					return fail(err)
				}
				var b strings.Builder
				for _, r := range repos {
					name := r.ID
					if name == "" {
						name = r.Source
					}
					if r.Readable {
						fmt.Fprintf(&b, "%s: %d items\n", name, r.Items)
					} else {
						fmt.Fprintf(&b, "%s: unreadable (%s)\n", name, r.Error)
					}
				}
				return strings.TrimRight(b.String(), "\n"), false
			},
		},
		{
			Name: "board_items",
			Description: "List work items on the team board. Each line is `<id> [status] holder=<who|-> <title>`; \"(you)\" marks items you hold. " +
				"Use the id exactly as shown in board_claim and board_release. holder: \"none\" for unclaimed, " +
				"\"me\" for yours, or an identity.",
			InputSchema: obj(map[string]any{
				"repo":   str("only items of this repo id, e.g. billing-api"),
				"status": str("only this status: open, in_progress or blocked"),
				"holder": str("none | me | <identity>"),
				"limit":  map[string]any{"type": "integer", "description": "max lines (default 40)"},
			}),
			Handler: func(raw json.RawMessage) (string, bool) {
				var a struct {
					Repo, Status, Holder string
					Limit                int
				}
				_ = json.Unmarshal(raw, &a)
				if a.Limit <= 0 {
					a.Limit = 40
				}
				items, err := c.items()
				if err != nil {
					return fail(err)
				}
				if a.Holder == "me" {
					if a.Holder = c.who(); a.Holder == "" {
						return "could not determine your identity from relai-server", true
					}
				}
				var out []Item
				for _, i := range items {
					switch {
					case a.Repo != "" && i.Repo != a.Repo:
					case a.Status != "" && i.Status != a.Status:
					case a.Holder == "none" && i.Holder != "":
					case a.Holder != "" && a.Holder != "none" && i.Holder != a.Holder:
					default:
						out = append(out, i)
					}
				}
				var b strings.Builder
				fmt.Fprintf(&b, "%d items\n", len(out))
				for n, i := range out {
					if n == a.Limit {
						fmt.Fprintf(&b, "… %d more (narrow with repo/status/holder)\n", len(out)-n)
						break
					}
					b.WriteString(c.line(i) + "\n")
				}
				return strings.TrimRight(b.String(), "\n"), false
			},
		},
		{
			Name:        "board_claim",
			Description: "Claim a work item before starting it. Fails, naming the holder, if someone else already holds it — then pick other work.",
			InputSchema: obj(map[string]any{"id": str("item id exactly as listed, e.g. billing-api/bd/billing-api-x1")}, "id"),
			Handler: func(raw json.RawMessage) (string, bool) {
				var a struct{ ID string }
				_ = json.Unmarshal(raw, &a)
				if a.ID == "" {
					return "id is required", true
				}
				it, err := c.claim(a.ID)
				if err != nil {
					return fail(err)
				}
				return "Claimed: " + c.line(it), false
			},
		},
		{
			Name:        "board_release",
			Description: "Release an item you hold (done, or you will not finish it). Only the holder can release.",
			InputSchema: obj(map[string]any{"id": str("item id exactly as listed")}, "id"),
			Handler: func(raw json.RawMessage) (string, bool) {
				var a struct{ ID string }
				_ = json.Unmarshal(raw, &a)
				if a.ID == "" {
					return "id is required", true
				}
				it, err := c.release(a.ID)
				if err != nil {
					return fail(err)
				}
				return "Released: " + c.line(it), false
			},
		},
		{
			Name: "board_claim_next",
			Description: "Claim the first unclaimed, non-blocked item (optionally in one repo). Safe when several agents ask " +
				"at once: if an item was just taken it moves on to the next. Claims exactly one item.",
			InputSchema: obj(map[string]any{"repo": str("only consider this repo id")}),
			Handler: func(raw json.RawMessage) (string, bool) {
				var a struct{ Repo string }
				_ = json.Unmarshal(raw, &a)
				items, err := c.items()
				if err != nil {
					return fail(err)
				}
				var cand []Item
				for _, i := range items {
					if i.Holder == "" && i.Status != "blocked" && (a.Repo == "" || i.Repo == a.Repo) {
						cand = append(cand, i)
					}
				}
				// open work first, then work already marked in progress; stable within each group
				sort.SliceStable(cand, func(x, y int) bool { return (cand[x].Status == "open") && (cand[y].Status != "open") })
				for _, i := range cand {
					it, err := c.claim(i.ID)
					if err == nil {
						return "Claimed: " + c.line(it), false
					}
					if ae, ok := err.(*apiError); ok && (ae.Status == http.StatusConflict || ae.Status == http.StatusNotFound) {
						continue // taken or gone in the meantime
					}
					return fail(err)
				}
				return "No unclaimed, non-blocked item is available.", false
			},
		},
	}
}
