### Decisions
<!-- One line each: date — decision — why. -->
- 2026-11-03 — Separate repo from RELAI, not a mode flag — keeps RELAI's "git-only, no server" positioning intact; see README "Why a separate repo".
- 2026-11-03 — This first change (`team-server-foundation`) builds only the `team-server` capability (aggregation + claims + persistence + HTTP + auth). MCP interface and deployment presets are separate, later changes — don't fold their scope in here.
- 2026-11-03 — Storage: append-only JSON-lines event log, replayed into memory on startup, not SQLite — stdlib-only for v1, matches RELAI's own stdlib-first posture (see design.md Decisions).
- 2026-11-03 — Aggregation reads other repos via their CLIs/files (`bd list --json`, `openspec list --json`, `relai.yaml`) directly, not via RELAI's Go internal packages — Go's `internal/` visibility doesn't cross modules anyway, and this keeps the coupling to RELAI's data contract, not its code.
- 2026-11-03 — Packaging includes a Dockerfile + docker-compose.yml (task group 5) so a team can configure and run this without a Go toolchain — the binary itself has no required container, Compose is a convenience.

### Pitfalls
<!-- Things that cost time once and will again: symptom → cause → what to do. -->
- `bd init` may make its own commit and touch `.gitignore` → use `--skip-agents --skip-hooks`, then `git reset --soft HEAD~1` and recommit cleanly if it does.
- `bd create -q --parent <id>` with the epic ID captured from a prior `bd create -q` call: if that capture is empty/wrong, the children won't link — check with `bd children <epic-id> --json` and `bd link <child> <epic> --type parent-child` to fix.

### Next session
- 2026-11-03 — team-server-foundation at 9/13: groups 1-3 done; group 4 code (internal/server, cmd/relai-server) written and tested (22 tests, race-clean) but 4.3 unchecked: rerun the restart check against real repos (/tmp/relai-sim, config /tmp/relai-team-test/config.json), then group 5 (Dockerfile, compose) and 6 (README).
- Verified live on real repos: OpenSpec tasks aggregated, new Beads task appeared without rebuild, contended claim got 409 naming the holder.
- Use Go 1.23 (/opt/go1.23/bin): system Go 1.19 lacks method+path ServeMux patterns.
