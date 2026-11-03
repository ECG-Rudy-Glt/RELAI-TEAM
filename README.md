# relai-team

**Optional, self-hosted team server for [Relai](https://github.com/ECG-Rudy-Glt/RELAI).**

Relai itself is deliberately git-only — one CLI, no server, no running process. That's what makes
it adoptable by a single person with nothing but git. `relai-team` is the separate, optional layer
for a team that needs more than Relai's git-only mode gives them: a **live** (not rebuilt-on-demand)
view of work across several repos, and **claim/release coordination** so two agents don't collide
on the same task.

Relai never depends on this repo. This repo depends on Relai's repo contract (`relai.yaml`, Beads,
OpenSpec) — it has nothing to aggregate without it.

**Status:** v0.x, first slice implemented: aggregation, claim/release, persistence, HTTP API,
Docker packaging. Spec and task breakdown: `openspec/changes/team-server-foundation/`.

## What it does

- `relai-server`: a single binary that reads a declared list of repos, aggregates each one's
  `relai.yaml`, Beads tasks and OpenSpec change tasks into work items, and serves a live board over
  HTTP. It re-reads the repos on a timer, so a status change shows up without a rebuild.
- Claim a work item so another identity sees it is taken; release it when done. Exactly one claim
  wins under contention; the loser gets `409` naming the current holder.
- Board state survives restarts (an append-only JSON-lines event log, `events.jsonl` — no external
  database). Only claims are persisted; items are always re-read from the repos.
- Token-based identity: every request except `/healthz` must carry `Authorization: Bearer <token>`
  of a declared team member.

Not in this first slice: an MCP interface and deployment presets are planned as separate, later
OpenSpec changes in this repo.

## Quick start

### With Docker

```sh
mkdir -p config data
cat > config/config.json <<'EOF2'
{
  "listen": ":8080",
  "data_dir": "/data",
  "refresh_interval": "30s",
  "repos": ["/repos/auth-service", "/repos/billing-api"],
  "members": [{"identity": "alice", "token": "change-me"}]
}
EOF2
RELAI_TEAM_REPOS=$HOME/work docker compose up -d     # $HOME/work/auth-service is /repos/auth-service
curl -H 'Authorization: Bearer change-me' localhost:8080/items
```

- The image bundles `git`, `bd` (Beads) and `openspec`, which the aggregator reads through.
- `RELAI_TEAM_REPOS` is mounted at `/repos` **read-write**: `bd` writes lock files in `.beads/`
  even for a read. The container runs as `RELAI_TEAM_UID:RELAI_TEAM_GID` (default `1000:1000`);
  set them to the owner of your repos so no root-owned files appear in them.
- `./data` must be writable by that same user. If you first ran as root, `events.jsonl` is
  root-owned: fix its ownership or remove it.
- `RELAI_TEAM_PORT` changes the published port (default 8080).
- Claims survive `docker compose restart`.

### Bare binary

Needs Go 1.23+ (method+path `ServeMux` patterns), plus `git`, `bd` and `openspec` on `PATH`
for the parts you want read.

```sh
go build -o relai-server ./cmd/relai-server
./relai-server -config config.json        # or: RELAI_TEAM_CONFIG=config.json ./relai-server
```

## Config file (JSON)

| Field | Default | Meaning |
|---|---|---|
| `listen` | `:8080` | Address to listen on. |
| `data_dir` | `data` | Holds `events.jsonl`. |
| `refresh_interval` | `30s` | How often repos are re-read (Go duration). |
| `repos` | — | Local paths or clone URLs (shallow-cloned on each refresh). |
| `members` | — | `{"identity", "token"}` or `{"identity", "token_sha256"}` (hex SHA-256, so the file holds no plaintext secret). |

A repo that cannot be read is reported (`readable: false`, with the reason), never silently
dropped and never filled with stale data.

## HTTP API

All routes except `/healthz` need `Authorization: Bearer <token>`.

| Route | Result |
|---|---|
| `GET /healthz` | `{"status":"ok"}` (no auth) |
| `GET /me` | Your identity. |
| `GET /repos` | Declared repos with `readable`, `items`, `notes`. |
| `GET /repos/{repo}/items` | Items of one repo; `404 repo not tracked` if undeclared. |
| `GET /items`, `GET /items/{id}` | Work items with `holder` and `claimed_at`. |
| `POST /items/{id}/claim` | `200` with you as holder; `409` + current holder if taken. |
| `POST /items/{id}/release` | `200`; `409` if someone else holds it. |

Item IDs contain slashes (`auth-service/bd/auth-service-0p2`): **URL-encode them** in the path
(`auth-service%2Fbd%2Fauth-service-0p2`), or you get a 404.

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
