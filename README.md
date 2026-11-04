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

![The relai-team web board: four columns, who holds what, live activity](docs/images/board.png)

<sub>Ten seconds of a team working on six fictional projects. Dark mode follows your system:
[`docs/images/board-dark.png`](docs/images/board-dark.png); filtered to one agent:
[`docs/images/board-filtered.png`](docs/images/board-filtered.png).</sub>

## What you get

- **One live board** over every declared repo, with a **web page** (`/board`) that updates the moment
  something changes. A task closed in Beads, or ticked in an OpenSpec `tasks.md`, shows up within one
  refresh cycle (default 30 s).
- **Easy on your disk.** Unchanged repos cost nothing: no process is started and nothing is written
  (see "Why it does not poll Beads").
- **Claim / release with a guarantee.** Exactly one claim wins when several agents ask at once; the
  others are told who holds it. Claims **lapse** after a lease (default 8 h) unless renewed, so a
  crashed agent never blocks a task, and an administrator can free one by hand.
- **Agent tools (MCP).** `relai-mcp` gives Claude Code, Codex and other MCP clients five tools to
  list, claim and release work, as their own identity.
- **Claims survive restarts.** An append-only event log (`events.jsonl`), no database to run. A write that fails half way (disk full) is cut off, and a line that is not a valid event is moved to `events.jsonl.rejected` with a warning instead of stopping the server.
- **Per-person tokens, optional HTTPS.** Every request is authenticated as a declared team member;
  set a certificate and key and the server speaks HTTPS itself.
- **Honest about gaps.** A repo the server cannot read is reported as unreadable, never silently
  dropped or shown stale.

## Quick start

You need `git`, plus `bd` ([Beads](https://github.com/steveyegge/beads)) and `openspec` for the parts you
want shown. Then three commands:

```sh
go install github.com/ECG-Rudy-Glt/RELAI-TEAM/cmd/relai-server@latest   # or: go build ./cmd/relai-server

relai-server init --scan ~/work          # finds your repos that have Beads / OpenSpec / relai.yaml
relai-server -config ./config.json       # then open http://localhost:8080/board
```

`init` writes `config.json` (mode 0600) with one random token per member and prints the tokens **once**:
only their hashes are stored. It refuses to overwrite an existing config.

```text
$ relai-server init --scan ~/work --members alice,agent-1,agent-2
Wrote /home/you/config.json (6 repos, 3 members)

Tokens. Shown once: the config keeps only their hashes. Give each person theirs.
  alice            rt_a432…  (administrator)
  agent-1          rt_29a2…
  agent-2          rt_7be0…

Next
  1. relai-server -config /home/you/config.json
  2. open http://localhost:8080/board and paste a token
  3. for an agent:  claude mcp add relai-team -e RELAI_TEAM_URL=http://localhost:8080 -e RELAI_TEAM_TOKEN=rt_7be0… -- relai-mcp
```

Options: `--repos a,b` (paths or clone URLs) instead of or besides `--scan`, `--members`, `--listen`,
`--dir`, `--force`. The first member is the administrator.

**The board is empty?** The server reads repos through `bd` and `openspec`; if one is not on its `PATH` the repo
is still listed but its tasks are not, and the server says so at startup (`WARNING: bd is not on PATH…`), in the
web board (the "with warnings" pill and a note under each repo) and in `GET /repos`. Install the tool, or use the
Docker image, which bundles all three.

**Nothing to try it on?** Make fictional projects first:

```sh
scripts/demo-repos.sh /tmp/demo           # six small repos with Beads tasks and OpenSpec changes
relai-server init --scan /tmp/demo --dir /tmp/demo-board
relai-server -config /tmp/demo-board/config.json
```

### With Docker (no Go, no bd, no openspec on your machine)

```sh
mkdir -p config data
# either `relai-server init`, or write the file by hand:
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

Needs Go 1.21+ (the module pins toolchain 1.26.10, which Go downloads by itself), and `git`, `bd` and `openspec` on `PATH` for the parts you want read.

```sh
go build -o relai-server ./cmd/relai-server
./relai-server -config config.json        # or: RELAI_TEAM_CONFIG=config.json ./relai-server
```

## Web board

Open `http(s)://<host>:8080/board` (the server root redirects there). The page holds no data: it asks
for your token, keeps it in that browser tab only (`sessionStorage`, forgotten when the tab closes),
and then reads the same authenticated API as everything else. Columns: *Blocked*, *In progress*,
*Claimed*, *Open*; filters by repo and by person; an activity feed of who claimed or released what.
It updates live over the event stream, and falls back to polling if the stream is cut. It is
read-only: claiming is done by people through the API and by agents through MCP.

## Agents (MCP)

Each agent runs its own `relai-mcp`, pointed at the shared server with **its own token**, so every
claim is attributed to the right identity.

```sh
go install github.com/ECG-Rudy-Glt/RELAI-TEAM/cmd/relai-mcp@latest

claude mcp add relai-team -e RELAI_TEAM_URL=http://localhost:8080 -e RELAI_TEAM_TOKEN=change-me-2 -- relai-mcp
```

Over HTTPS with a self-signed or private certificate, add `RELAI_TEAM_CA_FILE=/path/to/cert.pem`.
`relai-mcp` warns on stderr if you point it at plain `http://` on another machine.

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
| `board_claim` | Claim one item. If someone holds it, the answer names them: pick other work. Claiming your own item again renews it; your items show when their claim lapses. |
| `board_release` | Release an item you hold. Administrators can pass `force` to free someone else's (a crashed agent's, say). |
| `board_claim_next` | Claim the first unclaimed, non-blocked item (optionally in one repo). Safe when many agents ask at once. |
| `board_find` | Before building something: which repos provide or consume a capability (case-insensitive) and who they work with. |
| `board_links` | How the repos relate: provides, consumes, declared links; with `candidates` the mentions found in their files (evidence, not verified). |
| `board_changes` | What happened since a sequence number, in a few lines instead of listing every item again. Call it once for the number, then with `since=<number>`. |

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
| `claim_lease` | `8h` | How long a claim lasts unless its holder claims again to renew it. `0s`: claims never lapse. |
| `git_credentials` | — | For private repos declared by clone URL: `[{"host":"github.com","token_env":"GITHUB_TOKEN"}]`. The token is read from that environment variable, never from this file. Optional `username` (default `x-access-token`; GitLab wants `oauth2`). See "Private repos". |
| `git_ssh_key`, `git_known_hosts` | — | Private key (and optionally a `known_hosts` file to verify against) for `git@…` / `ssh://` remotes. |
| `refresh_interval` | `30s` | How often repos are *checked* for changes (Go duration, at least 1s). Checking is cheap; only changed repos are read. |
| `full_refresh_interval` | `6h` | Longest an *unchanged* repo is trusted before being read again anyway (safety net). `0s` reads every repo every cycle: expensive, see below. |
| `refresh_concurrency` | `8` | How many changed repos are read at once. |
| `tls_cert`, `tls_key` | — | PEM files; set both to serve HTTPS (TLS 1.2+), or neither. |
| `repos` | — | Local paths or clone URLs; at least one. A clone URL is only cloned when its remote HEAD moved. The same repository twice (also through a symlink) is refused, and so is a second repo reusing another's `relai.yaml` `id` (it is shown as unreadable, with the reason). |
| `issue_mirrors` | — | GitHub or GitLab projects whose open issues appear as items, read-only. See "Issues from GitHub or GitLab". |
| `discover` | — | Folders whose git repos join the board automatically: `[{"folder": "/repos", "depth": 1}]` (depth 1-5). Re-scanned at each refresh. See "Repos from a folder". |
| `auth_failures_per_minute`, `requests_per_minute`, `trust_proxy` | `10`, `600`, `false` | Rate limits: wrong or missing tokens per client address, and requests per member (`0` turns one off); `trust_proxy` reads the client address from the last `X-Forwarded-For` entry, only behind a proxy that sets it. |
| `members` | — | Identities are 1-64 letters, digits, `.` `_` `-` `@`; a plain `token` is at least 16 characters (`relai-server init` generates 48). `{"identity", "token"}` or `{"identity", "token_sha256"}` (hex SHA-256, so the file holds no plaintext secret), and `"admin": true` to let that member force-release others' items. |

Config is read at startup; restart to change members or repos. Unknown fields are refused (a typo such as `claim_leas` is an error, not a silent default), and `claim_lease` is `0s` or at least `1s`.

## HTTP API

All routes except `/healthz` need `Authorization: Bearer <token>`.

| Route | Result |
|---|---|
| `GET /healthz` | `{"status":"ok","ready":true}` (no auth). The server listens before the first read of the repos: until it finishes, `ready` is `false` and the other routes answer `503` with `Retry-After`. |
| `GET /me` | Your identity. |
| `GET /repos` | Declared repos with `readable`, `items`, `notes`. |
| `GET /repos/{repo}/items` | Items of one repo; `404 repo not tracked` if undeclared. |
| `GET /items`, `GET /items/{id}` | Work items with `holder` and `claimed_at`. |
| `POST /items/{id}/claim` | `200` with you as holder and `expires_at`; `409` + current holder if taken. Claiming an item you already hold **renews** it. |
| `POST /items/{id}/release` | `200`; `409` if someone else holds it. Add `?force=true` as an administrator to free an item someone else holds (`403` for anyone else; the log records who did it). |
| `GET /events` | Server-Sent Events stream: one `change` event whenever the board changes (no board data in it: re-fetch `/items`). |
| `GET /changes?since=N` | What happened after sequence number N (claims, releases, new or changed items), the current `seq`, and `complete: false` when N is older than the 2,000 events kept (then list `/items` again). Without `since`: the current `seq` and the latest 10. | With `net=1` only the last change of each item is kept (claimed and released ten times = one line); `board_changes` always asks for it.
| `GET /board` | The web board (public, no data in the page). |

Item IDs contain slashes (`auth-service/bd/auth-service-0p2`): **URL-encode them** in the path
(`auth-service%2Fbd%2Fauth-service-0p2`), or you get a 404. (`relai-mcp` does this for you.)

## Claims, leases and force-release

A claim lasts `claim_lease` (default 8 h) from the moment it was made. While it lasts the item shows
who holds it and when it lapses; after that it counts as unclaimed and anyone can take it. The holder
renews by **claiming the item again** (agents are told this by the tool text). So a crashed or closed
agent frees its tasks by itself, with no clean-up job: the lapse is computed when the board is read,
from the time already in the event log, which makes it identical after a restart.

An administrator (`"admin": true` on the member; `init` makes the first member one) can free an
item held by someone else with `board_release … force` or `POST …/release?force=true`. Nobody else
can, and an administrator who does not ask for `force` is treated like any member. The log keeps both
names (the former holder and who freed it).

## Private repos

Repos declared by clone URL can be private. Tell the server how to authenticate per host, and keep the
secret itself in the environment:

```json
{
  "repos": ["https://github.com/acme/billing-api.git", "git@github.com:acme/infra.git"],
  "git_credentials": [{ "host": "github.com", "token_env": "GITHUB_TOKEN" }],
  "git_ssh_key": "/run/secrets/id_ed25519",
  "git_known_hosts": "/run/secrets/known_hosts"
}
```

```sh
GITHUB_TOKEN=ghp_… relai-server -config config.json       # Docker: put it in config/secrets.env
```

- The token is sent only over **HTTPS**, as a request header scoped to that exact host: never in a URL,
  a command line, a log or an API answer. A repo URL that contains a password or token
  (`https://user:pw@…`) is refused at startup, since the URL is shown to every member.
- For GitHub use a fine-grained token with read access to *Contents*; for GitLab set `"username": "oauth2"`.
- git never waits for a person: a missing or wrong credential fails at once and `/repos` says why
  (`authentication required for github.com: no credential configured`, `authentication refused … check
  the token in $GITHUB_TOKEN`), without ever printing the token.
- Without `git_known_hosts`, SSH accepts a host's key the first time it sees it (weaker); with it, keys
  are verified strictly.
- A private repo is cloned only when its remote HEAD moved (`git ls-remote`, no disk writes), like the
  rest of the change-driven refresh.

## Repos from a folder, and how they relate

You do not have to list the repos by hand. Point the server at the folder where your projects live (in Docker, the volume mounted
at `/repos`):

```json
{ "discover": [{ "folder": "/repos", "depth": 1 }] }
```

Every git repository there (symbolic links to repositories included, each once) joins the board, and the folder is looked at again at
each refresh: **a project you clone into it appears by itself**. A repository that disappears becomes unreadable but keeps its items
and claims. `repos` and `discover` can be used together; at least one of `repos`, `discover` or `issue_mirrors` is required.

The board also reads each repo's `relai.yaml`: `provides:` and `consumes:` (capability names) and `links:` (ids of the repos it works with).

- `GET /links` returns the repos with those fields and the **edges** that follow: *declared* (a person wrote `links:`) and *capability*
  (a repo consumes what another provides). With `?candidates=1` it also returns the **mentions** found in the files of the repos on
  the server's disk: strong = the other repo's remote URL or a relative path to it, weak = its name as a word (with a count of those in
  CI, deployment or config files, which are far more often real). Mentions are **evidence, not links**: they come back with
  `declared: false` and a note, and only a person writes `links:` in a `relai.yaml` (`relai discover --links --propose` prints a proposal).
  The search runs `git grep` between every pair of repos, read-only, and is cached for 30 minutes; repos read by clone URL are not searched.
- `GET /find?capability=auth.oidc` (case-insensitive) says which repos provide or consume it and who they work with.
- MCP: `board_find` ("does the team already have this?") and `board_links`. Not finding something means "nobody declared it", not "it does not exist".

## Issues from GitHub or GitLab (read-only mirror)

Projects that keep their tasks as GitHub or GitLab issues (no Beads) can still appear on the board:

```json
{
  "issue_mirrors": [
    { "provider": "github", "project": "acme/billing-api", "token_env": "GITHUB_ISSUES_TOKEN" },
    { "provider": "gitlab", "project": "group/sub/web", "token_env": "GITLAB_ISSUES_TOKEN",
      "base_url": "https://gitlab.example.com", "repo_id": "web", "interval": "10m" }
  ]
}
```

```sh
GITHUB_ISSUES_TOKEN=github_pat_… GITLAB_ISSUES_TOKEN=glpat-… relai-server -config config.json   # Docker: config/secrets.env
```

- **It only reflects the current state.** Open issues become items named `<repo_id>/gh/<number>` (GitHub) or
  `<repo_id>/gl/<iid>` (GitLab), kind `issue`. Labels `in progress` / `doing` / `wip` make the item `in_progress`,
  `blocked` makes it `blocked`. Pull requests are left out. **The server never writes to the forge**: a claim lives
  in the team's own journal and the issue is not assigned, labelled or commented. Closing an issue removes the item.
- **A read-only token is enough**: a fine-grained GitHub token with *Issues: read* on those repositories, or a GitLab token
  with `read_api`. It is read from the environment variable, sent only to the configured host over **HTTPS**
  (plain `http://` is accepted for a loopback test server only), never followed through a redirect to another host,
  and never printed in a log, an error or an API answer. An empty variable stops the server at start.
- **It asks politely.** At most one request per `interval` (default 5 minutes, minimum 1 minute), and an unchanged list
  costs a conditional request (`ETag`, `304`). The list is sorted by last update, so any edit changes the first page;
  a closed older issue is only noticed at the next full re-read, every 6 hours. At most the 300 most recently updated
  open issues are shown (`/repos` says so). On a rate limit the mirror stops asking until the limit is over.
- If the forge cannot be reached, the items already shown stay and `/repos` says why the source is unreadable.
- `/repos` lists each mirror under its source (`github:acme/billing-api`). `repo_id` must differ between mirrors and from
  the `id` in any repo's `relai.yaml` (a clash is reported, as for two repos with the same id).

## HTTPS

```json
{ "tls_cert": "/etc/relai-team/cert.pem", "tls_key": "/etc/relai-team/key.pem" }
```

Both or neither (the server refuses to start with only one). Listening beyond loopback **without**
them logs a warning at startup, because tokens then travel in clear. A reverse proxy that terminates
TLS works just as well; the event stream needs response buffering off (`X-Accel-Buffering: no` is
already sent for nginx). For a quick self-signed certificate:

```sh
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes -keyout key.pem -out cert.pem \
  -days 365 -subj /CN=localhost -addext "subjectAltName=DNS:localhost,IP:127.0.0.1"
```

## Why it does not poll Beads

Every `bd` run rewrites its embedded Dolt database on disk, about 2–3 MiB of journal, index and
manifest, **even for a read, even with `--readonly`**. A server that ran `bd list` on every repo at
every refresh was measured at ~9.5 MB/s of disk writes (~790 GiB/day) with nothing happening, enough
to heat an NVMe drive to 80 °C. So relai-team never polls Beads blindly:

- A repo is **fingerprinted** with a few `stat` calls (no process): `relai.yaml`, `.beads/last-touched`
  (Beads rewrites it on every write and never on a read) and `openspec/changes`. `bd` and `openspec`
  run only when that fingerprint changes, so a task change appears within one `refresh_interval` and an
  idle board writes nothing.
- A clone URL is checked with `git ls-remote` (no disk writes) and cloned only when its HEAD moved.
- `full_refresh_interval` (default 6 h) re-reads every repo anyway, in case something changed that the
  fingerprint cannot see. A repo whose Beads reading fails is not retried every cycle.
- The server logs `refresh: re-read N repo(s) whose files changed` only when something changed, and `refresh: safety re-read of N repo(s)` when the `full_refresh_interval` net re-read repos whose files had not changed. A `full_refresh_interval` under one minute prints a warning at start. If you see that
  line constantly, something is rewriting those repos' files.

Setting `full_refresh_interval` to `0s` turns all of this off. Do not leave it on a laptop.

## Know before you deploy

- **Claims live in the server's log, not in Beads.** A `bd update --claim` done by hand is not seen
  as a claim on the board.
- **One active instance, with a standby for failover.** State is held in memory and one log file; a second server on the same `data_dir` is refused (an exclusive lock on `events.jsonl.lock`; not enforced on Windows). To survive a crash or a host reboot without a person, run a second process with `relai-server -standby` on the same `data_dir`: it serves nothing and waits for the lock, which the kernel releases when the active one stops or dies, then takes over from the same journal (claims kept). Both need the same `data_dir` on a filesystem that supports `flock` (the same host, or one volume). This is failover, not load balancing: there is still one server at a time.
- **Tokens do not expire, but they rotate without downtime.** `relai-server token -config config.json -identity alice` adds a new token (shown once) while the old one keeps working; hand it out; run it again with `-drop-old`; apply each step with `kill -HUP <pid>` (members, tokens and request limits reload; repos, address and TLS need a restart). Git and issue-mirror tokens are environment variables: change them and restart.
- **Rate limits** (see Security): wrong tokens per address and requests per member, in memory per server.
- **The web board is read-only**, and protected by the same tokens: use HTTPS for anything beyond a
  private network.
- **First read cost.** Starting the server reads every repo once (about 2.5 s for ten repos on four
  cores, in parallel); after that, only changes.

## Security

relai-team holds your team's board and the tokens that identify its members, and it runs `git`, `bd` and
`openspec` inside the repos you declare. In short:

- Every route except `/healthz`, `/board` and `/` needs a member token. `relai-server init` stores only the
  hashes. Wrong tokens are rate limited per address (the token is not looked at past the limit) and each member is capped; tokens rotate without a restart (`relai-server token`, `kill -HUP`).
- **Use HTTPS** (`tls_cert`/`tls_key`, or a TLS proxy) for anything beyond a private network. Plain HTTP
  sends tokens in clear, and the server warns at startup when it listens beyond loopback.
- Declared repos are trusted input, and **task titles are untrusted text that reaches AI agents** (through
  MCP and the board): treat them as data, never as instructions.
- Give each person and each agent its own member; keep administrators to a minimum.

The full model, what is and is not protected, a hardening checklist, and how to report a vulnerability
privately are in **[SECURITY.md](SECURITY.md)**. Please do not open a public issue for a security problem.

## Status

v0.x. Done: aggregation (parallel, change-driven), claims, persistence, token auth, HTTP API, event
stream, web board, HTTPS, claim leases and force-release, private repos, `init`, Docker packaging,
MCP tools.
Not started: deployment presets (deliberately deferred — see `.relai/notes.md`).
Specs and history live in `openspec/` (`specs/` for what is true now, `changes/archive/` for how it
got there); open tasks are in Beads (`bd ready`).

## Why a separate repo, not a mode of Relai

It keeps Relai's "git only, no server" promise intact instead of turning it into "git only,
except…". It matches Relai's own *compose, don't rebuild* principle: this project reads Relai's
repo contract (`relai.yaml`, Beads, OpenSpec) through their own CLIs and files, with no code
coupling back. And it lets this idea succeed or fail on its own without touching Relai's
credibility. Details: `openspec/changes/archive/*-team-server-foundation/proposal.md`.

## Contributing

Contributions are welcome, from people and from AI agents, under the same rules. **[CONTRIBUTING.md](CONTRIBUTING.md)**
has the details; the short version:

```sh
git clone https://github.com/ECG-Rudy-Glt/RELAI-TEAM && cd RELAI-TEAM
bd bootstrap --yes          # a fresh clone has no tasks until this loads .beads/issues.jsonl (needs bd)
go test -race ./...         # also before each PR: gofmt -l . (empty) and go vet ./...
bd ready                    # what is unblocked; scripts/sync-beads.sh after changing tasks
```

- Bugs and ideas: open an issue (security problems: see above). Docs that disagree with the behaviour are bugs.
- Bigger changes start as an OpenSpec change under `openspec/changes/`; small fixes do not need one.
- Conventional commits, no co-author trailers, tests with the change, README kept true.
- **Never poll `bd` on a timer**: each run rewrites ~3 MiB on disk (the reason the server fingerprints repos).
- Using Claude Code? `CLAUDE.md` and a session-start hook load the project's state automatically; other
  agents start from `AGENTS.md`.

## Licence

[Apache-2.0](LICENSE).
