# Tasks

## 1. MCP protocol core (`internal/mcp`)

- [x] 1.1 Implement the stdio JSON-RPC loop (`initialize`, `notifications/initialized`, `ping`,
      `tools/list`, `tools/call`, unknown method/tool errors, malformed JSON); verify with tests
      that drive it through in-memory pipes, including that stdout carries only protocol messages.

## 2. Board tools

- [x] 2.1 Add an HTTP client for the team API (bearer token, URL-encoded IDs) and the tools
      `board_repos`, `board_items`, `board_claim`, `board_release`; verify with `httptest` for the
      spec scenarios: attribution, unknown token, lost race, releasing another's item, server down
      then back.
- [x] 2.2 Add `board_claim_next`; verify with a test where N goroutines call it at once against a
      real in-process server (`-race`) and each gets a different item, plus the nothing-available
      case.

## 3. Binary and docs

- [x] 3.1 Add `cmd/relai-mcp/main.go` (env config, clear startup error when unset); verify by
      piping a real `initialize` + `tools/call` session into the built binary against a running
      `relai-server`.
- [x] 3.2 README section: build, ready-to-paste MCP config for Claude Code and Codex, token
      handling note.

## 4. Verification with real agents

- [x] 4.1 Run at least two real AI agents (not scripted) against the fixture repos through the
      adapter and confirm they claim distinct items and respect each other's claims; record what
      happened in `.relai/notes.md`.

## Workflow follow-up

- Archive once tasks 1-4 are done and reviewed.
