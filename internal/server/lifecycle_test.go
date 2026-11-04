package server

import (
	"bufio"
	"net/http"
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
