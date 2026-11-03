# Proposal

## Why

This repository exists to build exactly one thing: an optional, self-hosted team server that
gives a team a live, shared view of work across the repos of one ecosystem, plus the coordination
primitives (claim/release a work item) that [RELAI](https://github.com/ECG-Rudy-Glt/RELAI)'s
git-only CLI deliberately does not provide. Full rationale, market positioning, and the three
capabilities this project covers (`team-server`, `mcp-team-interface`, `deployment-presets`) are
in RELAI's own `docs/DESIGN.md` §11/§12 and the `team-server-mode` OpenSpec change archived there.

This first change builds only the foundation capability, `team-server`: aggregation and claim
semantics over HTTP. `mcp-team-interface` (the MCP surface) and `deployment-presets` are separate,
later changes in this repo — each independently implementable once this foundation exists, since
both consume the same board/claim state this change creates.

## What Changes

- A single binary, `relai-server`: reads a declared list of repos, aggregates each one's
  `relai.yaml`/Beads/OpenSpec state into work items, and serves that as a live board over HTTP.
- Claim/release semantics: a work item has at most one holder at a time; a claim under contention
  resolves to exactly one winner.
- Embedded, file-based persistence (no external database): board state survives a restart.
- Token-based identity: requests must authenticate as a member of the declared team; unauthorized
  or unauthenticated requests are rejected.
- Packaging: a `Dockerfile` and `docker-compose.yml` so a team can configure and run the server
  with one command, without needing a Go toolchain.

## Capabilities

### New Capabilities
- `team-server`: a long-running, multi-user process that aggregates the contract
  (`relai.yaml`/Beads/OpenSpec) of a declared set of repos into a live board of work items, tracks
  which identity holds which item, and persists that state across restarts.

### Modified Capabilities
(none — first change in this repo)

## Impact

- **Code**: `cmd/relai-server` (entrypoint), `internal/aggregate` (reads declared repos' contract
  files — its own reader, not a dependency on RELAI's internal packages, since Go's `internal/`
  visibility doesn't cross modules and this project should only couple to RELAI's *data contract*,
  not its code), `internal/board` (work items, holder, claim/release, compare-and-swap),
  `internal/store` (append-only persistence), `internal/server` (HTTP API, auth).
- **Docs**: `README.md` (quick start, config), this change's own docs.
- **Non-code**: `Dockerfile`, `docker-compose.yml`.
- **Tracking**: Beads epic in this repo (see `.beads/`), mirroring this change's tasks so multiple
  agents can pick up independent task groups via `relai handoff` / `bd ready`.
