# relai-team — instructions for AI agents

Start here:
1. Run `relai handoff` (current state, next tasks, decisions, pitfalls — a few hundred tokens).
   `relai` not installed or not found? Read `.relai/notes.md`, `relai.yaml`, and
   (if present) `.relai/index.json` directly instead — they are plain files, no CLI required.
2. Before building something another project might already provide, check
   `relai find <capability>` (or read `.relai/index.json` directly) — don't duplicate
   what already exists elsewhere.
3. Load anything else only when you need it.

## Rules
- Keep this file under 100 lines.
- Record decisions and pitfalls in `.relai/notes.md`, not in chat.

## This repo
- Go, standard library only unless a dependency is clearly worth it. So far there are none: the
  event log is plain JSON-lines and the MCP adapter is hand-rolled JSON-RPC (see the archived
  `mcp-team-interface` design for why).
- Before committing: `gofmt -l .` (must be empty), `go vet ./...`, `go test ./...`.
- Everything is written in English (code, docs, commits, issues).
- Commit messages: conventional style (`feat:`, `fix:`, `docs:`…), no Co-Authored-By or
  "Generated with" trailers.
- Design lives in the active OpenSpec change under `openspec/changes/`; tasks through Beads
  (`bd ready`).
- This is a *separate, optional* project from [RELAI](https://github.com/ECG-Rudy-Glt/RELAI) — it
  consumes RELAI's repo-contract format (`relai.yaml`, Beads, OpenSpec) but RELAI never depends on
  this repo. Keep it that way: no code coupling back into RELAI, only reading its declared contract
  files and CLIs (`bd ready --json`, `openspec list --json`).
