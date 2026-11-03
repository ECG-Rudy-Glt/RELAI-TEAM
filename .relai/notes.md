### Decisions
<!-- One line each: date — decision — why. -->
- 2026-11-03 — Separate repo from RELAI, not a mode flag — keeps RELAI's "git-only, no server" positioning intact; see README "Why a separate repo".
- 2026-11-03 — This first change (`team-server-foundation`) builds only the `team-server` capability (aggregation + claims + persistence + HTTP + auth). MCP interface and deployment presets are separate, later changes — don't fold their scope in here.
- 2026-11-03 — Storage: append-only JSON-lines event log, replayed into memory on startup, not SQLite — stdlib-only for v1, matches RELAI's own stdlib-first posture (see design.md Decisions).
- 2026-11-03 — Aggregation reads other repos via their CLIs/files (`bd list --json`, `openspec list --json`, `relai.yaml`) directly, not via RELAI's Go internal packages — Go's `internal/` visibility doesn't cross modules anyway, and this keeps the coupling to RELAI's data contract, not its code.
- 2026-11-03 — Packaging includes a Dockerfile + docker-compose.yml (task group 5) so a team can configure and run this without a Go toolchain — the binary itself has no required container, Compose is a convenience.
- 2026-11-03 — MCP adapter is a separate stdio binary (`relai-mcp`) that calls the HTTP API with the agent's own token, hand-rolled JSON-RPC, no SDK — one identity per agent session, server untouched, repo stays stdlib-only (see archived `mcp-team-interface` design).
- 2026-11-03 — `deployment-presets` is deferred, not rewritten: its spec was lost and its purpose ("trigger deployments from the board or MCP") means remote command execution on shared infrastructure, which needs real requirements (which environments, who may deploy, what runs, audit) rather than a guess. Revisit only with those answers. Ticket RELAI-TEAM-9pg is `deferred`.


- 2026-11-03 — Repos are fingerprinted and only read when their files change (Beads' `.beads/last-touched`, `relai.yaml`, `openspec/changes`; `git ls-remote` for clone URLs), with a 6 h safety re-read — because every `bd` run rewrites its Dolt database (~2-3 MiB), which at one read per repo per refresh wrote ~9.5 MB/s and overheated an NVMe (see Pitfalls).
- 2026-11-03 — Web board is a static page at `/board` that holds no data and takes a pasted token (sessionStorage), live over SSE (`/events`, fetch-streaming so it can send the Authorization header); read-only on purpose.
- 2026-11-03 — TLS is in-process and optional (`tls_cert`/`tls_key`); plain HTTP beyond loopback only logs a warning, so private-network setups keep working.

- 2026-11-03 — Claims are leased (default 8 h, `claim_lease`; `0s` = never): expiry is computed when read from the claim event's time, not scheduled, so it is the same after a restart; the holder renews by claiming again; admins (`admin: true`, first member from `init`) force-release with `?force=true`, logged as `by`. Default chosen so a dead agent frees a task within a working day.
- 2026-11-03 — Private clone URLs: tokens come from an environment variable named in `git_credentials[].token_env` (never in the config), sent only over HTTPS as a URL-scoped `http.<url>.extraHeader` via `GIT_CONFIG_*` env (not in argv, not in the URL, not in the clone's remote); git always runs with prompts off so it fails fast; URLs with userinfo are refused at startup because the source is shown to every member.
- 2026-11-03 — `relai-server init` (+ `internal/initcfg`) writes config with random tokens, stores only SHA-256, refuses to overwrite; `scripts/demo-repos.sh` makes fictional projects; `scripts/screenshots.js` retakes the README images.

- 2026-11-03 — Tasks travel with the repo as `.beads/issues.jsonl` (via `scripts/sync-beads.sh`, owner e-mail removed); a fresh clone runs `bd bootstrap --yes`. Before this, every task of this repo existed only in one machine's local Dolt database.
- 2026-11-03 — `CLAUDE.md` (`@AGENTS.md`) and a SessionStart hook make Claude Code start from the handoff; verified with real `claude -p` in a fresh clone (1 turn with the hook, 2 without).
- 2026-11-03 — SECURITY.md and CONTRIBUTING.md added; private vulnerability reporting is NOT enabled on the GitHub repo, so SECURITY.md has a fallback (a detail-free public issue). Enabling it is a repo setting for the owner.

### Pitfalls
<!-- Things that cost time once and will again: symptom → cause → what to do. -->
- `bd init` may make its own commit and touch `.gitignore` → use `--skip-agents --skip-hooks`, then `git reset --soft HEAD~1` and recommit cleanly if it does.
- `bd create -q --parent <id>` with the epic ID captured from a prior `bd create -q` call: if that capture is empty/wrong, the children won't link — check with `bd children <epic-id> --json` and `bd link <child> <epic> --type parent-child` to fix.
- Agents do not know their own identity name: a holder shown as `bob` on their own claim made a real agent doubt it was theirs → items held by the caller are marked `(you)` (found by the real-agent test).
- A background launcher's processes can die when the launching shell task ends, even with `setsid nohup`: if a demo server "disappears", check `pgrep -f relai-server` before debugging the code.
- Real-agent test recipe: `claude -p "<prompt>" --mcp-config <file> --strict-mcp-config --allowedTools 'mcp__relai-team__board_*' --max-budget-usd 0.4`, one config per agent with its own `RELAI_TEAM_TOKEN`; run from a neutral directory. ~$0.20 per agent per short task.


- **NEVER poll `bd` on a timer.** Each `bd` call (even `list --json --readonly`) rewrites ~2-3 MiB of `.beads/embeddeddolt/.../noms/{journal,journal.idx,manifest}`. The first version of the server did this for every repo every refresh: measured 9.5 MB/s (~790 GiB/day) idle, an NVMe at 77-80 °C (host normally 44-48 °C). Measure writes with `awk '$3=="sda"{print $10*512}' /proc/diskstats` around `sync`, not with a few sequential calls: a short sequential test showed only 5 KiB/call and hid it. `openspec` and `git` write nothing. Beads' `last-touched` changes on writes (create/update/close/show), not on reads: that is the usable signal.
- `rm -rf`-style or self-matching `pkill -f '<pattern>'` inside the same command line kills your own shell (exit 144): use `pkill -f '[p]attern'` and never put the literal pattern in a command that also launches it.
- A response `WriteTimeout` on the HTTP server kills long-lived SSE streams: lifted per request with `http.NewResponseController(w).SetWriteDeadline(time.Time{})` in `/events` only.

### Measurements (2026-11-03, 4 cores, 10 fixture repos, ~3 MiB of Dolt writes per `bd` run)
- First full read: 6.4 s sequential -> 2.5 s parallel (8 workers); 3.2 s with 4, 2.3 s with 10: CPU-bound by `bd`.
- Idle server, refresh 2 s, 30 s window: 9.5 MB/s before the cache -> at the machine's noise floor (26 vs 32 KiB/s with no server at all) after.
- A real `bd update` is visible in 1.8 s with one repo re-read.

- `git http-backend` is a real, scriptable git server for tests: serve it with `net/http/cgi` behind a Basic-auth wrapper (see `internal/aggregate/gitauth_test.go`), `t.Setenv("GIT_SSL_CAINFO", ca)` so git trusts an `httptest` TLS cert. It proved a wrong token makes git retry a prompt, which looks like "no credential" unless the code knows one was configured.
- A rebuilt `relai-mcp` is needed after changing the tools: a stale binary in /tmp gave "force not honoured" in a manual run while the tests passed.

- `bd export` puts the creator's e-mail in every record: never commit a raw export, use `scripts/sync-beads.sh`.
- Tasks that only exist in `.beads/embeddeddolt` are lost on a new machine: always run `scripts/sync-beads.sh` and commit after changing tasks.

### Next session
- 2026-11-03 — `team-server-foundation` and `mcp-team-interface` done, archived, verified (unit + race tests, real stdio session, 3 real Claude Code agents: 3+1+2 of 6 items, no duplicate).
- Known gaps: claims are not mirrored to Beads assignee; no token rotation (restart to change tokens); the web board is read-only; no UI for admins to force-release (API/MCP only); `deployment-presets` deferred.
- `deployment-presets` stays deferred until someone states the requirements (see Decisions).
- Use Go 1.23 (/opt/go1.23/bin): system Go 1.19 lacks method+path ServeMux patterns.
- Docker pitfalls: `bd` writes lock files even to read, so repos are mounted read-write and the container runs as the repos' owner (RELAI_TEAM_UID/GID); a root run leaves a root-owned `data/events.jsonl` that the non-root container can't open. Item IDs contain `/`: URL-encode them in paths.
