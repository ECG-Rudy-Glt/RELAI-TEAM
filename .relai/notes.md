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

### Next session
- 2026-11-03 — `team-server-foundation` and `mcp-team-interface` done, archived, verified (unit + race tests, real stdio session, 3 real Claude Code agents: 3+1+2 of 6 items, no duplicate).
- Known gaps, in priority order: claim expiry / admin force-release (a crashed agent keeps its item), HTTPS guidance (reverse proxy), parallel repo reads (refresh ≈ 1 s per repo), a built-in web board (`GET /board`), clone-URL repos untested, claims not mirrored to Beads assignee.
- `deployment-presets` stays deferred until someone states the requirements (see Decisions).
- Use Go 1.23 (/opt/go1.23/bin): system Go 1.19 lacks method+path ServeMux patterns.
- Docker pitfalls: `bd` writes lock files even to read, so repos are mounted read-write and the container runs as the repos' owner (RELAI_TEAM_UID/GID); a root run leaves a root-owned `data/events.jsonl` that the non-root container can't open. Item IDs contain `/`: URL-encode them in paths.
