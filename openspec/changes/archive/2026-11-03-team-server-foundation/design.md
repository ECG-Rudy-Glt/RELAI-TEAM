# Design

## Context

This repo is new; nothing exists yet. It implements one capability, `team-server`, specified in
`specs/team-server/spec.md`. The broader positioning and alternatives already considered are in
RELAI's `docs/DESIGN.md` §11/§12 and the `team-server-mode` change archived there — this document
only restates the decisions that shape *this* change's implementation, plus Docker packaging,
which RELAI's own design didn't cover.

## Goals / Non-Goals

**Goals:**
- A team can run one binary (or `docker compose up`) and get a live, persistent, authenticated
  board aggregated from repos that already follow RELAI's repo contract.
- No second source of truth: the server reads `relai.yaml`, Beads (`bd list --json`), and OpenSpec
  (`openspec list --json`) the same way RELAI's own CLI does, via their own CLIs/files — not a
  Go-level dependency on RELAI's `internal/` packages, which Go's module boundary doesn't allow
  across modules anyway.
- Claims are correct under real concurrency (compare-and-swap at the storage layer, not
  check-then-write in application code).

**Non-Goals (this change):**
- The MCP interface (`mcp-team-interface`) and deployment presets (`deployment-presets`) — separate
  changes, built on top of this one's board/claim state.
- Any specific auth provider (OIDC, SSO) — token-based identity is enough to satisfy the spec's
  "authenticate before reading or modifying" requirement; richer auth is a later change if ever
  needed.
- A hosted/SaaS variant. This is a binary (and a Compose file) a team runs itself.

## Decisions

**Storage: an append-only JSON-lines event log, replayed into an in-memory board on startup.**
Every state change (repo declared, work item created/claimed/released) is appended as one JSON
record; `internal/store` replays the log into `internal/board`'s in-memory state when
`relai-server` starts. Alternative considered: SQLite via a pure-Go driver — rejected for this
first change to stay stdlib-only like the rest of the project (`AGENTS.md`); revisit if the log
file's replay cost becomes a real problem, same deferral posture RELAI itself uses elsewhere.

**Claim semantics: an in-memory mutex-guarded compare-and-swap on the work item's holder field,
with the winning claim appended to the log before the response is sent.** This is the simplest
implementation that satisfies the spec's "exactly one claim succeeds" scenario; a future move to
SQLite would replace the mutex with a DB-level CAS without changing the HTTP API or the spec.

**Repo aggregation reads each declared repo via its local path or a shallow clone (same convention
as RELAI's `relai index build`), not a live git/API connection.** `internal/aggregate` shells out
to `git`, `bd list --json`, `openspec list --json` in each declared repo's directory, exactly how
RELAI's own `internal/handoff` and `internal/index` already do it — proven, already-tested
behavior, not reinvented here.

**HTTP API: standard library `net/http` with Go 1.22+ method+path `ServeMux` patterns, no router
dependency.** The API surface (list repos, list items, claim/release) is small enough that a
third-party router isn't a "clearly worth it" dependency per `AGENTS.md`'s stdlib-only default.

**Identity: a static token → identity map loaded from a config file at startup.** Satisfies the
spec's authentication/authorization requirements without picking a specific external auth
provider; a `Bearer <token>` header maps to a declared team member. Swappable later without
changing the spec.

**Packaging: a `Dockerfile` building `relai-server` into a minimal image, plus a
`docker-compose.yml` that mounts the declared-repos config and the data directory as volumes and
exposes the HTTP port.** This is packaging convenience, not an architecture requirement — the
binary runs standalone with no container at all; Compose exists so a team can configure and start
the server with one command and one config file, without installing Go. No second container
(database, proxy) is needed since storage is an embedded file and the server serves HTTP directly.

## Risks / Trade-offs

- **Log replay cost grows with history.** Mitigation: out of scope for a first team-scale
  deployment; add log compaction/snapshotting only if a real deployment shows it matters.
- **Static token auth is weak compared to OIDC/SSO.** Mitigation: explicitly scoped as sufficient
  for the spec's requirements, not as a long-term security posture; flagged as a later change if a
  team needs stronger auth.
- **Aggregation depends on `bd`/`openspec` being installed and each declared repo being reachable
  on disk (or clonable).** Mitigation: same posture as `relai index build` — skip and report a
  repo the server can't read, never fail the whole aggregation.

## Open Questions

(none blocking this change — see the broader open questions already recorded in RELAI's
`team-server-mode` design.md, which apply once `mcp-team-interface`/`deployment-presets` start.)
