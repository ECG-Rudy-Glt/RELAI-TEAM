# Design

## Context

`relai-server` already provides authenticated, atomic claim/release over HTTP. MCP is a thin
JSON-RPC 2.0 protocol; the stdio transport is newline-delimited JSON on stdin/stdout.

## Goals / Non-Goals

**Goals:** agents can discover, claim and release work through MCP tools; a lost race is an
ordinary, recoverable result; no second implementation of any board rule.

**Non-Goals:** a hosted/HTTP MCP transport, resources/prompts, claim leases, deployment actions.

## Decisions

**A separate adapter binary, not a mode of `relai-server`.** An MCP client launches its server as a
subprocess per session and each session needs its own identity, so the adapter runs next to the
agent and calls the shared server over HTTP with that agent's token. The server stays unchanged.

**Hand-rolled stdio JSON-RPC, no SDK.** The subset needed (`initialize`, `ping`,
`tools/list`, `tools/call`, the `initialized` notification) is ~150 lines. This keeps the repo
stdlib-only. Revisit if resources, prompts or streamable HTTP are ever needed.

**Configuration through environment** (`RELAI_TEAM_URL`, `RELAI_TEAM_TOKEN`), because that is what
MCP client config files support everywhere and it keeps the token out of the command line.

**Failures are tool results, not protocol errors.** `isError: true` with a plain sentence
(`already held by alice`, `repo not tracked`, `server unreachable`) lets the agent react. JSON-RPC
errors are reserved for malformed requests and unknown tools.

**`board_claim_next` exists because "list, pick, claim" races.** With several agents, the item an
agent picked is often gone by the time it claims. The tool walks the candidates and claims the
first one the server accepts, so the agent gets one item or a clear "nothing available".

**IDs are passed verbatim** (`repo/bd/id`); the adapter URL-encodes them.

## Risks / Trade-offs

- A tool that claims on the agent's behalf makes it easy to hoard. Mitigation: `board_claim_next`
  claims one item per call and tool descriptions tell agents to release what they will not do.
- A stdio adapter per session means N processes polling nothing (it only reacts to calls), which is
  cheap; but the token sits in the client's MCP config — document that.
- No claim expiry yet: a crashed agent keeps its item until released. Tracked separately.
