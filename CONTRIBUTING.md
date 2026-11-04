# Contributing to relai-team

Thanks for helping. This is a small project run by one maintainer, built for humans **and** AI agents to
work on together: the rules below are the same for both.

## Ways to help

- **Report a bug or a rough edge**: open an issue with what you ran, what you expected, what happened,
  and `relai-server version`. Logs and config with the tokens removed help a lot.
- **Report a security problem**: not as a public issue, see [SECURITY.md](SECURITY.md).
- **Improve the docs**: if the README told you something that was not true, that is a bug.
- **Pick up a task**: see "Picking a task" below.
- **Try it with another MCP client or agent** and tell us what confused it: the tool descriptions
  are part of the product.

## Set up

You need Go 1.21+ (the module pins toolchain 1.26.10, which Go downloads by itself), `git`, and for the full test and demo experience `bd` ([Beads](https://github.com/steveyegge/beads))
and `openspec` (Node 22.12+). Docker is only needed to test the image.

```sh
git clone https://github.com/ECG-Rudy-Glt/RELAI-TEAM && cd RELAI-TEAM
bd bootstrap --yes                 # loads the task list from .beads/issues.jsonl (a fresh clone has none)
go build ./... && go test -race ./...
scripts/demo-repos.sh /tmp/demo    # six fictional projects to run the server against
go run ./cmd/relai-server init --scan /tmp/demo --dir /tmp/demo-board
go run ./cmd/relai-server -config /tmp/demo-board/config.json      # then open /board
```

If you use Claude Code, `CLAUDE.md` and a session-start hook load the project's state for you; other
agents should read `AGENTS.md`.

## Before you open a pull request

```sh
gofmt -l .            # must print nothing
go vet ./...
go test -race ./...
```

- **Standard library only** unless a dependency is clearly worth it (there are none today).
- **Tests come with the change.** A bug fix gets a test that fails without it. Behaviour described in
  `openspec/specs/` needs a test per scenario.
- **Everything is in English**: code, docs, commits, issues.
- **Commit messages** are conventional (`feat:`, `fix:`, `docs:`, `test:`, `chore:`) and carry no
  `Co-Authored-By` or "Generated with" trailers (a project rule, also for AI-written commits).
- **Keep the README true.** If a command, flag or limit changes, change the README in the same PR.
- **One concern per PR**, with a description that says what changed, why, and how you verified it.

### Merge rules

These apply to every pull request, whoever (or whatever) wrote it. The PR template carries the checklist.

1. **Green CI is required.** `gofmt`, `go vet`, `go test` and the Docker build must pass; a red check is fixed, not skipped.
2. **Nothing merges without a review from the code owner** (`.github/CODEOWNERS`). The author of a PR never merges it
   themselves, and an AI agent never merges, tags or releases unless the maintainer asked it to in that session.
3. **One concern per PR**, small enough to review in one sitting. A fix found on the way goes in its own PR.
4. **Branch from `main`, named for the change** (`feat/…`, `fix/…`, `docs/…`). No force-push on a branch someone reviewed;
   add commits, the history is squashed or merged at the end.
5. **Security-sensitive areas need extra care**: the journal (`internal/store`), authentication and rate limits
   (`internal/server`), the issue mirror (`internal/mirror`). A change there says in the PR what it can break and how it was
   tested against an attacker, not only against a friend.
6. **The PR description is the record.** It states what changed, why, how it was verified, and what was not verified.
   If a number is quoted (tokens, seconds, savings), it says how many runs it comes from.
7. **After the merge**, the author closes the tasks with `Closes <task id>` in the merge commit message and removes the branch.

Recommended repository settings (the maintainer sets them; they are not enforced by this file): protect `main`, require the `test`
and `docker` checks, require one approving review and a review from code owners, dismiss stale approvals on new commits, forbid
force-pushes and deletion of `main`, and require branches to be up to date before merging.

### Larger changes go through OpenSpec

A new feature or a change in behaviour starts as a change under `openspec/changes/<name>/`: a
`proposal.md` (why), `design.md` (decisions and trade-offs), spec deltas under `specs/` (what must be true,
as WHEN/THEN scenarios) and `tasks.md` (small steps, each with how to verify it). Validate with
`openspec validate <name>`; when the work is done, `openspec archive <name>` merges the deltas into
`openspec/specs/`. Small fixes and doc changes do not need one. If unsure, open an issue first.

### Picking a task

Tasks live in Beads, not in GitHub issues: `bd ready` lists what is unblocked, `bd show <id>` the detail.
The task list reaches other people through `.beads/issues.jsonl`, so after creating, updating or closing a
task run `scripts/sync-beads.sh` and commit that file (it strips the personal owner field).

## One rule that protects hardware: never poll `bd`

Every `bd` run rewrites about 3 MiB of its database on disk, even for a read. An early version of the server
ran it for every repo on every refresh and wrote ~9.5 MB/s (about 790 GiB a day) while idle, enough to
overheat an SSD. So: never call `bd` in a loop or on a timer; read repos through the change-driven path
in `internal/aggregate` (fingerprint first, spawn only when it changed), and when you change anything that
reads repos, measure the disk writes of an idle server before and after
(`awk '$3=="sda"{print $10*512}' /proc/diskstats` around a `sync`).

## For AI agents

Start with `AGENTS.md`. Record decisions and pitfalls in `.relai/notes.md`, not in chat. Test with real
processes where it matters (the repo's own history shows how), say what you did **not** verify, and never
publish, merge or tag without being asked.

## Licence

By contributing you agree that your contribution is licensed under the [Apache License 2.0](LICENSE), the
same as the project (section 5 of the licence).
