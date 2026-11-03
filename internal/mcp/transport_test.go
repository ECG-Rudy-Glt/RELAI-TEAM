package mcp

import (
	"crypto/tls"
	"encoding/pem"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainHTTPWarning(t *testing.T) {
	for url, want := range map[string]bool{
		"http://localhost:8080": false, "http://127.0.0.1:8080": false, "http://[::1]:8080": false,
		"https://team.example": false, "http://team.example:8080": true, "http://10.0.0.5:8080": true,
	} {
		w := PlainHTTPWarning(url)
		if (w != "") != want {
			t.Errorf("PlainHTTPWarning(%q) = %q, want warning=%v", url, w, want)
		}
	}
}

func TestHTTPSWithCustomCA(t *testing.T) {
	h := startServer(t, 1)
	ts := httptest.NewUnstartedServer(h.Config.Handler)
	ts.TLS = &tls.Config{MinVersion: tls.VersionTLS12}
	ts.StartTLS()
	defer ts.Close()

	// Without the CA the self-signed certificate is rejected, as an error result.
	plain, err := HTTPClient("")
	if err != nil {
		t.Fatal(err)
	}
	c := &Client{BaseURL: ts.URL, Token: "alice-token", HTTP: plain}
	if txt, isErr := tool(t, c, "board_repos", `{}`); !isErr || !strings.Contains(txt, "unreachable") {
		t.Errorf("untrusted certificate = %v %q, want an error result", isErr, txt)
	}

	// With the server's certificate as CA it works.
	ca := filepath.Join(t.TempDir(), "ca.pem")
	der := ts.Certificate().Raw
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		t.Fatal(err)
	}
	trusting, err := HTTPClient(ca)
	if err != nil {
		t.Fatal(err)
	}
	c = &Client{BaseURL: ts.URL, Token: "alice-token", HTTP: trusting}
	if txt, isErr := tool(t, c, "board_repos", `{}`); isErr {
		t.Errorf("with the CA: %q", txt)
	}
}

func TestHTTPClientRejectsBadCAFile(t *testing.T) {
	if _, err := HTTPClient("/no/such/file.pem"); err == nil {
		t.Error("missing CA file accepted")
	}
	bad := filepath.Join(t.TempDir(), "bad.pem")
	_ = os.WriteFile(bad, []byte("not a certificate"), 0o644)
	if _, err := HTTPClient(bad); err == nil || !strings.Contains(err.Error(), "no certificate") {
		t.Errorf("garbage CA file: %v", err)
	}
}
