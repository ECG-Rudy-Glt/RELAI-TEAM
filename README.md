# relai-team

**Optional, self-hosted team server for [Relai](https://github.com/ECG-Rudy-Glt/RELAI).**

Relai itself is deliberately git-only — one CLI, no server, no running process. That's what makes
it adoptable by a single person with nothing but git. `relai-team` is the separate, optional layer
for a team that needs more than Relai's git-only mode gives them: a **live** (not rebuilt-on-demand)
view of work across several repos, and **claim/release coordination** so two agents don't collide
on the same task.

Relai never depends on this repo. This repo depends on Relai's repo contract (`relai.yaml`, Beads,
OpenSpec) — it has nothing to aggregate without it.

**Status:** early development (v0.x). Not implemented yet — see
`openspec/changes/team-server-foundation/` for the spec and task breakdown; `bd ready` for what's
unblocked right now.

## What it will do (once built)

- `relai-server`: a single binary that reads a declared list of repos, aggregates each one's
  `relai.yaml`/Beads/OpenSpec state into work items, and serves a live board over HTTP.
- Claim a work item (`POST /items/:id/claim`) so another identity sees it's taken; release it when
  done. Exactly one claim wins under contention.
- Board state persists across restarts (an embedded, append-only event log — no external database).
- Token-based identity: every request authenticates as a declared team member.
- `docker compose up` to configure and run it without a Go toolchain.

Not in this first slice: an MCP interface and deployment presets are planned as separate, later
OpenSpec changes in this repo, built on top of the board this change creates.

## Why a separate repo, not a mode of Relai

See `AGENTS.md` and `openspec/changes/team-server-foundation/proposal.md`. Short version: it keeps
Relai's "git-only, no server" promise intact rather than turning it into "git-only, except..."; it
matches Relai's own "compose, don't rebuild" principle (this repo composes with Relai's CLI/file
contract, Beads and OpenSpec the same way Relai composes with them); and it means this idea can
fail or succeed on its own without touching Relai's credibility.

## Contributing / picking up work

This repo uses Relai on itself. Start here:

```sh
relai handoff     # or: bd ready, if relai isn't installed — see AGENTS.md
```

Pick an unblocked task, do the work, commit, update `.relai/notes.md` for whoever picks this up
next.

## Licence

[Apache-2.0](LICENSE).
