// relai-mcp exposes a relai-server team board to AI agents as MCP tools (stdio).
//
//	RELAI_TEAM_URL    base URL of relai-server, e.g. http://localhost:8080
//	RELAI_TEAM_TOKEN  this agent's team-member token
//	RELAI_TEAM_CA_FILE (optional) PEM certificate(s) to trust, for a self-signed server
package main

import (
	"fmt"
	"os"

	"github.com/ECG-Rudy-Glt/RELAI-TEAM/internal/mcp"
)

var version = "dev"

func main() {
	url, token := os.Getenv("RELAI_TEAM_URL"), os.Getenv("RELAI_TEAM_TOKEN")
	if url == "" || token == "" {
		// stderr only: stdout is the protocol stream.
		fmt.Fprintln(os.Stderr, "relai-mcp: set RELAI_TEAM_URL and RELAI_TEAM_TOKEN (see README, \"Agents (MCP)\")")
		os.Exit(2)
	}
	if w := mcp.PlainHTTPWarning(url); w != "" {
		fmt.Fprintln(os.Stderr, w)
	}
	hc, err := mcp.HTTPClient(os.Getenv("RELAI_TEAM_CA_FILE"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "relai-mcp:", err)
		os.Exit(2)
	}
	s := &mcp.Server{Name: "relai-team", Version: version, Instructions: mcp.Instructions(), Tools: mcp.Tools(&mcp.Client{BaseURL: url, Token: token, HTTP: hc})}
	if err := s.Serve(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "relai-mcp:", err)
		os.Exit(1)
	}
}
