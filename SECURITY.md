# Security policy

relai-team is a server that holds a team's work board and the tokens that identify its members, and
that runs `git`, `bd` and `openspec` on repos you point it at. Please read this before exposing it
beyond your own machine.

## Reporting a vulnerability

**Do not open a public issue with details.**

1. Use GitHub's private report: the repository's **Security** tab → **Report a vulnerability**.
2. If that button is not there, open a public issue titled `Security: please contact me` with **no
   details at all**; the maintainer will arrange a private channel.

Please include what you ran, what you expected and what happened, the version (`relai-server version`)
and your configuration with the tokens removed. This is a small project with one maintainer: expect an
acknowledgement within about a week, a fix as soon as one can be made safely, and credit in the release
notes if you want it. There is no bug bounty.

## Supported versions

The project is v0.x. Only the latest release and `main` get fixes.

## What the server protects, and how

| Area | What it does | What it does *not* do |
|---|---|---|
| Identity | Every route except `/healthz`, `/board` and `/` needs `Authorization: Bearer <token>`. Tokens are looked up by their SHA-256, so response timing does not reveal a prefix of a valid token. `relai-server init` stores only hashes. | No token expiry or rotation (change the config and restart), no rate limiting, no lockout after failed attempts. |
| Roles | Two: member and administrator. Only an administrator can force-release another member's claim, and only when it asks for it explicitly; the log records who did. | No finer permissions: every member can read every declared repo's tasks and claim any item. |
| Transport | Optional built-in TLS (1.2+). Without it, tokens travel in clear: the server warns at startup when it listens beyond loopback. | It does not force HTTPS. Put TLS in front of anything that is not a private network. |
| Web board | The page is public but static and holds no data. You paste a token; it lives in that tab's `sessionStorage` only. Text coming from repos is escaped before display, and a strict Content-Security-Policy applies (`default-src 'none'`, no framing). | A token pasted into a page is only as safe as the HTTPS and the browser it sits in. |
| Git credentials | For private repos by clone URL: the token is read from an environment variable, sent only over HTTPS as a request header scoped to that host, and scrubbed from error messages. A URL carrying a password or token is refused at startup. | Static tokens are not rotated; SSH is only verified strictly if you give `git_known_hosts`. |
| Forge tokens (issue mirror) | A read-only token per mirrored project, read from an environment variable, sent only to the configured host over HTTPS, never through a redirect to another host, never in a log, error or API answer. The server never writes to GitHub or GitLab. | A leaked token gives read access to those issues: use a fine-grained token limited to *Issues: read* on the listed repositories. Issue titles come from anyone who can open an issue: they are stripped of control characters and cut to 200 characters, and the web page escapes them like any title. |
| Disk | Unchanged repos are not re-read, so an idle server writes nothing (see "Why it does not poll Beads" in the README). | A write that fails (disk full) is cut back so the log stays readable, a line that is not a valid event is moved to `events.jsonl.rejected` with a warning, and a second server on the same `data_dir` is refused by a lock. The event log is not signed or encrypted: anyone who can write the data directory can forge claims. |

## Things to keep in mind

- **Declared repos are trusted input.** The server runs `git`, `bd` and `openspec` inside them. Only
  declare repos you trust, as you would before running their build.
- **Task text is untrusted text that reaches AI agents.** Titles and OpenSpec content come from the
  repos and are passed verbatim to agents through MCP (`relai-mcp`) and the web board. A hostile repo can
  put instructions in a task title ("prompt injection"). Agents and prompts should treat board text as
  data, never as commands, and keep their own permissions tight.
- **A member's token is that member's whole identity.** An agent with a member's token can claim,
  release and, if it is an administrator, force-release in that member's name. Give agents their own
  members, and an administrator token only to people.
- **One instance per event log.** Two servers on the same log can corrupt state.
- **The MCP adapter keeps the token in the client's MCP config.** Treat that file like a credential.

## Hardening checklist for a team deployment

- Set `tls_cert` / `tls_key`, or terminate TLS in a reverse proxy (keep response buffering off for `/events`).
- Use one member per person and per agent; make as few administrators as you can.
- Generate tokens with `relai-server init` (random, hashes only in the file) and keep `config.json` at
  mode 0600. Do not commit it.
- Keep git tokens in the environment (`token_env`), or in `config/secrets.env` outside git (it is in
  `.gitignore`). Give them read-only scope to the repos you declare.
- Run the container as the repos' owner (the compose file already does) and mount only the repos and
  the data directory.
- Restrict who can reach the port: a VPN or a private network is better than the open internet.

## In scope

Authentication or authorization bypass, a token or credential leaking into a response, log or error,
injection into the web board, path or ID tricks that read other data, a way to make the server execute
something it should not, a way to corrupt or forge the event log without write access to it.

## Out of scope

Anything that needs write access to the config or data directory; a hostile repo that you chose to
declare doing what repos do to `git`/`bd`/`openspec`; denial of service by a holder of a valid token;
missing rate limiting (known, documented above); reports against unsupported versions.
