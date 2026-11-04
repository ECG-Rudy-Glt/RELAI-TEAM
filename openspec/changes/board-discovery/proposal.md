# Proposal

## Why

The point of the board is to drive several linked repositories from one central place, for teams. Until now the operator had
to write the list of repositories by hand, the board knew nothing of how they relate, and an agent about to build something
could not ask whether the team already has it. Relai (the CLI) now discovers repositories and the mentions between them
(`relai discover --links`); the server is where a team sees the result together.

## What Changes

- `discover: [{folder, depth}]` in the config: the git repositories of those folders (symbolic links included) join the board and are
  looked for again at every refresh, so a project cloned there appears without editing the config. Read-only.
- The board reads each repo's `provides:`, `consumes:` and `links:` from its `relai.yaml`.
- `GET /links` returns the repos with what they provide, consume and declare they work with, and the edges that follow: declared
  (a person wrote `links:`) and by capability (a repo consumes what another provides). With `?candidates=1` it also returns the
  **mentions** found in the files of repos on the server's disk (cached 30 minutes), marked as not verified.
- `GET /find?capability=x` and the MCP tools `board_find` and `board_links`: "does the team already have this, and who works with whom".

## Impact

- Additive. Mentions are never turned into links by the server: only a person writes `links:` in a `relai.yaml`.
