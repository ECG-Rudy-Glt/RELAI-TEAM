package server

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"
	"time"
)

// at lets a test move the clock the limiters use.
type at struct{ t time.Time }

func (a *at) now() time.Time { return a.t }

func getWith(t *testing.T, f *fixture, path, token, xff string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", f.http.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Retry-After")
}

func withClock(t *testing.T, f *fixture, cfg func(*Config)) *at {
	t.Helper()
	clock := &at{t: time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)}
	f.srv.clock = clock.now
	c := f.cfg
	cfg(&c)
	if err := f.srv.Reload(c); err != nil {
		t.Fatal(err)
	}
	return clock
}

func TestWrongTokensAreCutOffBeforeTheTokenIsLookedAt(t *testing.T) {
	f := newFixture(t)
	ten := 3
	clock := withClock(t, f, func(c *Config) { c.AuthFailuresPerMinute = &ten })
	for i := 0; i < 3; i++ {
		if code, _ := getWith(t, f, "/me", "guess-"+itoa(i), ""); code != 401 {
			t.Fatalf("guess %d = %d, want 401", i, code)
		}
	}
	code, retry := getWith(t, f, "/me", "guess-4", "")
	if code != 429 || retry == "" {
		t.Fatalf("after 3 wrong guesses: %d retry-after %q, want 429 with Retry-After", code, retry)
	}
	// The point of the limit: even the RIGHT token is not looked at while the address is cut off, so a guess
	// cannot be tried. (An honest client on the same address waits a minute at most.)
	if code, _ := getWith(t, f, "/me", "alice-token", ""); code != 429 {
		t.Errorf("a correct token from a cut-off address = %d, want 429", code)
	}
	clock.t = clock.t.Add(30 * time.Second)
	if code, _ := getWith(t, f, "/me", "alice-token", ""); code != 200 {
		t.Errorf("after 30 s one token is back and a correct token should get in: %d", code)
	}
	// /healthz and the board page are not behind it
	if code, _ := getWith(t, f, "/healthz", "", ""); code != 200 {
		t.Errorf("healthz = %d", code)
	}
}

func TestOnlyTrustedProxiesCanChooseTheAddressTheLimitSees(t *testing.T) {
	f := newFixture(t)
	two := 2
	withClock(t, f, func(c *Config) { c.AuthFailuresPerMinute = &two }) // trust_proxy off
	// Changing X-Forwarded-For must not give a fresh budget when the proxy is not trusted.
	getWith(t, f, "/me", "bad1", "10.0.0.1")
	getWith(t, f, "/me", "bad2", "10.0.0.2")
	if code, _ := getWith(t, f, "/me", "bad3", "10.0.0.3"); code != 429 {
		t.Errorf("a client must not escape the limit by sending X-Forwarded-For: %d", code)
	}
	withClock(t, f, func(c *Config) { c.AuthFailuresPerMinute = &two; c.TrustProxy = true })
	getWith(t, f, "/me", "bad1", "1.1.1.1, 10.0.0.1")
	getWith(t, f, "/me", "bad2", "1.1.1.1, 10.0.0.1")
	if code, _ := getWith(t, f, "/me", "bad3", "9.9.9.9, 10.0.0.1"); code != 429 {
		t.Errorf("behind a trusted proxy the LAST entry is the client, earlier ones are the client's own words: %d", code)
	}
	if code, _ := getWith(t, f, "/me", "alice-token", "9.9.9.9, 10.0.0.2"); code != 200 {
		t.Errorf("another client address is not affected: %d", code)
	}
}

func TestOneMemberCannotFloodTheServer(t *testing.T) {
	f := newFixture(t)
	five := 5
	clock := withClock(t, f, func(c *Config) { c.RequestsPerMinute = &five })
	for i := 0; i < 5; i++ {
		if code, _ := getWith(t, f, "/me", "alice-token", ""); code != 200 {
			t.Fatalf("request %d = %d", i, code)
		}
	}
	if code, retry := getWith(t, f, "/me", "alice-token", ""); code != 429 || retry == "" {
		t.Errorf("6th request in a minute = %d (%q), want 429", code, retry)
	}
	if code, _ := getWith(t, f, "/me", "root-token", ""); code != 200 {
		t.Errorf("another member must not be affected by alice's flood: %d", code)
	}
	clock.t = clock.t.Add(time.Minute)
	if code, _ := getWith(t, f, "/me", "alice-token", ""); code != 200 {
		t.Errorf("a minute later alice is back: %d", code)
	}
	zero := 0
	withClock(t, f, func(c *Config) { c.RequestsPerMinute = &zero; c.AuthFailuresPerMinute = &zero })
	for i := 0; i < 50; i++ {
		if code, _ := getWith(t, f, "/me", "alice-token", ""); code != 200 {
			t.Fatalf("with the limits off request %d = %d", i, code)
		}
	}
}

func TestRotationAddsANewTokenBeforeRemovingTheOldAndKeepsTheClaims(t *testing.T) {
	f := newFixture(t)
	if code, obj, _ := f.do("POST", "/items/billing-api%2Fbd%2Fb-1/claim", "alice-token"); code != 200 || obj["holder"] != "alice" {
		t.Fatalf("claim before rotation: %d %v", code, obj)
	}
	newSum := sha256.Sum256([]byte("alice-new-token-0123456789"))
	rotated := f.cfg
	rotated.Members = append([]Member(nil), f.cfg.Members...)
	rotated.Members[0].TokenSHA256s = []string{hex.EncodeToString(newSum[:])} // step 1: both tokens valid
	if err := f.srv.Reload(rotated); err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"alice-token", "alice-new-token-0123456789"} {
		if code, obj, _ := f.do("GET", "/me", tok); code != 200 || obj["identity"] != "alice" {
			t.Errorf("during rotation token %q = %d %v, want both to be alice", tok, code, obj)
		}
	}
	step2 := rotated
	step2.Members = append([]Member(nil), rotated.Members...)
	step2.Members[0].Token = "" // step 2: the old one is gone
	if err := f.srv.Reload(step2); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := f.do("GET", "/me", "alice-token"); code != 401 {
		t.Errorf("the old token must be refused after step 2: %d", code)
	}
	if code, obj, _ := f.do("GET", "/items/billing-api%2Fbd%2Fb-1", "alice-new-token-0123456789"); code != 200 || obj["holder"] != "alice" {
		t.Errorf("a reload must not lose the claim: %d %v", code, obj)
	}
}

func TestReloadRefusesABrokenConfigAndKeepsTheOldOne(t *testing.T) {
	f := newFixture(t)
	bad := f.cfg
	bad.Members = []Member{{Identity: "x"}} // no token
	if err := f.srv.Reload(bad); err == nil {
		t.Fatal("a member without a token was accepted")
	}
	if code, _, _ := f.do("GET", "/me", "alice-token"); code != 200 {
		t.Errorf("a refused reload must leave the running configuration alone: %d", code)
	}
}
