package server

import (
	_ "embed"
	"net/http"
)

//go:embed web/board.html
var boardHTML []byte

// board serves the web board. The page is static and holds no board data: the person enters their
// token and the page then calls the authenticated API like any other client.
func (s *Server) boardPage(w http.ResponseWriter, _ *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	_, _ = w.Write(boardHTML)
}
