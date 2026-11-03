# relai-team

**A self-hosted team board for humans and AI agents working across many repos.**
It reads the work that already lives in git (Beads tasks, OpenSpec changes), shows it as one live
board, and lets each person or agent *claim* a task so two of them never start the same one.

No new place to keep tasks: the repos stay the source of truth, the board is a view of them plus a
claim register. It is the optional team layer for [Relai](https://github.com/ECG-Rudy-Glt/RELAI) —
Relai itself stays git-only with no server, and never depends on this project.

```
  repo A  (relai.yaml, .beads, openspec/)  ─┐
  repo B  (relai.yaml, .beads, openspec/)  ─┼─►  relai-server  ◄── HTTP (bearer token) ── people, scripts
  repo C  …                                ─┘   board + claims            ▲
                                                 events.jsonl             │ stdio
                                                                  relai-mcp ◄── Claude Code, Codex, …
```

## What you get

- **One live board** over every declared repo. A task closed in Beads, or ticked in an OpenSpec
  `tasks.md`, shows up on its own after the next refresh (default 30 s).
- **Claim / release with a guarantee.** Exactly one claim wins when several agents ask at once; the
  others are told who holds it.
- **Agent tools (MCP).** `relai-mcp` gives Claude Code, Codex and other MCP clients five tools to
  list, claim and release work, as their own identity.
- **Claims survive restarts.** An append-only event log (`events.jsonl`), no database to run.
- **Per-person tokens.** Every request is authenticated as a declared team member.
- **Honest about gaps.** A repo the server cannot read is reported as unreadable, never silently
  dropped or shown stale.

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
  "members": [
    {"identity": "alice",          "token": "change-me-1"},
    {"identity": "agent-claude-1", "token": "change-me-2"}
  ]
}
EOF2
RELAI_TEAM_REPOS=$HOME/work docker compose up -d     # $HOME/work/auth-service → /repos/auth-service
curl -H 'Authorization: Bearer change-me-1' localhost:8080/items
```

- The image bundles `git`, `bd` (Beads) and `openspec`, which the server reads repos through.
- `RELAI_TEAM_REPOS` is mounted **read-write**: `bd` writes lock files in `.beads/` even to read.
  The container runs as `RELAI_TEAM_UID:RELAI_TEAM_GID` (default `1000:1000`); set them to the owner
  of your repos so no root-owned files appear in them. `./data` must be writable by that user too.
- `RELAI_TEAM_PORT` changes the published port (default 8080). Claims survive `docker compose restart`.

### Bare binary

Needs Go 1.23+, and `git`, `bd` and `openspec` on `PATH` for the parts you want read.

```sh
go build -o relai-server ./cmd/relai-server
./relai-server -config config.json        # or: RELAI_TEAM_CONFIG=config.json ./relai-server
```

## Agents (MCP)

Each agent runs its own `relai-mcp`, pointed at the shared server with **its own token**, so every
claim is attributed to the right identity.

```sh
go install github.com/ECG-Rudy-Glt/RELAI-TEAM/cmd/relai-mcp@latest

claude mcp add relai-team -e RELAI_TEAM_URL=http://localhost:8080 -e RELAI_TEAM_TOKEN=change-me-2 -- relai-mcp
```

Any MCP client that can launch a stdio server works: the command is `relai-mcp` and the two
environment variables are `RELAI_TEAM_URL` and `RELAI_TEAM_TOKEN`. As a project `.mcp.json`:

```json
{ "mcpServers": { "relai-team": {
    "command": "relai-mcp",
    "env": { "RELAI_TEAM_URL": "http://localhost:8080", "RELAI_TEAM_TOKEN": "change-me-2" } } } }
```

| Tool | What it does |
|---|---|
| `board_repos` | Declared repos, item counts, which ones could not be read. |
| `board_items` | Work items, filterable by `repo`, `status`, `holder` (`none`, `me`, or an identity). `(you)` marks your own. |
| `board_claim` | Claim one item. If someone holds it, the answer names them: pick other work. |
| `board_release` | Release an item you hold. Only the holder can. |
| `board_claim_next` | Claim the first unclaimed, non-blocked item (optionally in one repo). Safe when many agents ask at once. |

Refusals come back as ordinary tool results, not crashes, so an agent just moves on.
The token lives in the client's MCP config: treat that file like any other secret.

**Tried with real agents:** three Claude Code agents, each with its own token, were told at the same
moment to take up to 3 tasks from a repo with 6. They ended with 3 + 1 + 2 tasks, no task twice,
and the two that ran out were told so cleanly.

## Configuration (JSON)

| Field | Default | Meaning |
|---|---|---|
| `listen` | `:8080` | Address to listen on. |
| `data_dir` | `data` | Holds `events.jsonl`. |
| `refresh_interval` | `30s` | How often repos are re-read (Go duration). |
| `repos` | — | Local paths or clone URLs (shallow-cloned on each refresh). |
| `members` | — | `{"identity", "token"}` or `{"identity", "token_sha256"}` (hex SHA-256, so the file holds no plaintext secret). |

Config is read at startup; restart to change members or repos.

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
(`auth-service%2Fbd%2Fauth-service-0p2`), or you get a 404. (`relai-mcp` does this for you.)

## Know before you deploy

- **No HTTPS.** Tokens travel in clear over plain HTTP. Put it behind a reverse proxy that
  terminates TLS, or keep it on a private network (VPN, Tailscale…).
- **No claim expiry.** If an agent crashes while holding a task, the task stays held until that same
  identity releases it. There is no admin override yet.
- **Refresh cost grows with repos.** Repos are read one after another (`bd` and `openspec` each
  take about half a second): ten repos is roughly a 10-second cycle.
- **Claims live in the server's log, not in Beads.** A `bd update --claim` done by hand is not seen
  as a claim on the board.
- **Single instance.** State is held in memory and one log file; do not run two servers on one log.
- **Not yet exercised:** repos declared by clone URL (including private ones — no git credentials
  are handled).
- **No web UI** in this repo yet; the API and MCP tools are the interface.

## Status

v0.x. Done: aggregation, claims, persistence, token auth, HTTP API, Docker packaging, MCP tools.
Not started: deployment presets (deliberately deferred — see `.relai/notes.md`).
Specs and history live in `openspec/` (`specs/` for what is true now, `changes/archive/` for how it
got there); open tasks are in Beads (`bd ready`).

## Why a separate repo, not a mode of Relai

It keeps Relai's "git only, no server" promise intact instead of turning it into "git only,
except…". It matches Relai's own *compose, don't rebuild* principle: this project reads Relai's
repo contract (`relai.yaml`, Beads, OpenSpec) through their own CLIs and files, with no code
coupling back. And it lets this idea succeed or fail on its own without touching Relai's
credibility. Details: `openspec/changes/archive/*-team-server-foundation/proposal.md`.

## Contributing / picking up work

This repo uses Relai on itself. Start here:

```sh
relai handoff     # or: bd ready, if relai isn't installed — see AGENTS.md
```

Pick an unblocked task, do the work, commit (conventional style), update `.relai/notes.md` for
whoever picks this up next. Before committing: `gofmt -l .` (empty), `go vet ./...`, `go test ./...`.

## Licence

[Apache-2.0](LICENSE).
