# Proposal

## Why

The board is only usable by agents that know to `curl` it, with item IDs they must URL-encode by
hand. Agents (Claude Code, Codex, …) speak MCP, so the board has to be reachable as tools: list
work, claim it before starting, release it when handing off. Without that, claim/release
coordination exists but no agent will use it.

## What Changes

- Add `relai-mcp`, a small stdio MCP server that exposes the board's HTTP API as tools. It holds no
  state and takes no decisions: every call goes to a running `relai-server` with the caller's own
  token, so identity, atomic claims and persistence stay where they already are.
- Tools: `board_repos`, `board_items`, `board_claim`, `board_release`, `board_claim_next`.
- Conflicts and refusals come back as tool results naming the holder, so an agent can pick other
  work instead of treating them as crashes.

## Capabilities

### New Capabilities
- `mcp-team-interface`: the agent-facing tool surface over the team board.

### Modified Capabilities
(none — `team-server` is unchanged; this is a client of its HTTP API)

## Impact

- **Code**: new `cmd/relai-mcp` and `internal/mcp`; no change to `relai-server`.
- **Docs**: README section with a ready-to-paste MCP config.
- **Out of scope**: `deployment-presets`, an HTTP/SSE MCP transport, claim expiry.
