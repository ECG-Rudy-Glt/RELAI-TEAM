# Tasks

## 1. Persistence (`internal/store`)

- [x] 1.1 Define the event log format (JSON-lines: repo declared, item created, item claimed, item
      released) and a writer that appends one record per call, fsyncing after each write; verify
      with a unit test that two sequential appends both land, in order, surviving a process
      restart (reopen the file and replay).
- [x] 1.2 Add a replay function that reads the log from the start and returns the ordered list of
      events; verify with a test covering an empty log, a log with one event, and a log with
      out-of-order-looking-but-chronological claim/release pairs on the same item.

## 2. Repo aggregation (`internal/aggregate`)

- [x] 2.1 Add a function that reads one declared repo's state (`relai.yaml`, `bd list --json` if
      `.beads/` exists, `openspec list --json` if `openspec/` exists) into a small struct, skipping
      and reporting what it can't read rather than failing; verify with a unit test against a
      fixture repo covering: full contract present, `.beads`/`openspec` absent, repo path missing.
- [x] 2.2 Add a function that aggregates a declared list of repos (same `relai index build`
      path/clone-URL convention) into work items (one per open Beads task and per active OpenSpec
      change task); verify with a test asserting the aggregated item count and fields match a
      multi-repo fixture.

## 3. Board (`internal/board`)

- [x] 3.1 Add the in-memory board: work items keyed by ID, each with a holder field (empty or an
      identity); replay persisted events (task 1.2) into this state on construction; verify with a
      test that a board replayed from a log with a claim event shows the right holder.
- [x] 3.2 Add `Claim(itemID, identity)` and `Release(itemID, identity)` with a compare-and-swap on
      the holder field (mutex-guarded), appending the resulting event to the store before
      returning success; verify with a test simulating two concurrent claims on the same item
      (goroutines + a start barrier) asserting exactly one succeeds and the loser gets the current
      holder's identity.
- [x] 3.3 Add `MergeAggregated(items)` that reconciles freshly aggregated items (task 2.2) into the
      board without clobbering an existing holder for an item that still exists, and drops items
      for work that's gone (e.g. a closed Beads task); verify with a test covering: a new item
      appears, an existing held item keeps its holder after re-aggregation, a closed task's item is
      removed.

## 4. HTTP server (`internal/server`, `cmd/relai-server`)

- [x] 4.1 Add token-based identity: a config-loaded token → identity map and a middleware that
      rejects a request with no or an unrecognized `Bearer` token before it reaches any handler;
      verify with a test for: valid token, missing token, unknown token.
- [x] 4.2 Add `GET /repos`, `GET /items`, `POST /items/{id}/claim`, `POST /items/{id}/release`
      using `net/http`'s method+path `ServeMux` patterns, backed by the board (task 3); verify with
      `httptest` covering an unknown repo (spec scenario: reports "not tracked", not stale/fake
      data) and the claim/release round trip.
- [x] 4.3 Add `cmd/relai-server/main.go`: loads a declared-repos config and the identity token map,
      constructs the store/board/aggregate wiring, runs aggregation on a timer (so the board
      reflects a task's status change without a manual rebuild, per the spec), and serves the HTTP
      API; verify by running the binary against a small fixture config and curling `/items`.

## 5. Packaging

- [x] 5.1 Add a multi-stage `Dockerfile` building a minimal `relai-server` image; verify with
      `docker build` producing an image that runs `relai-server` and answers `GET /repos`.
- [x] 5.2 Add `docker-compose.yml` mounting a config directory (declared repos, identity tokens)
      and a data directory (the event log) as volumes, exposing the HTTP port; verify with
      `docker compose up` against the same fixture config as 4.3, confirming the board is reachable
      and state survives `docker compose restart`.

## 6. Documentation

- [ ] 6.1 Write `README.md`: what this is, how it relates to RELAI, quick start (`docker compose
      up` and the bare-binary path), config file format, API surface; verify the documented
      commands match what tasks 4-5 implement.

## Workflow follow-up

- File Beads tasks mirroring groups 1-6 (`bd create` per group, or finer-grained to match the
  numbered tasks) so `relai handoff`/`bd ready` give each session a concrete next step.
- Archive this change once tasks 1-6 are complete and reviewed.
- `mcp-team-interface` and `deployment-presets` are separate, later changes — do not fold their
  scope into this one.
