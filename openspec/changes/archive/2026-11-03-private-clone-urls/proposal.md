# Proposal

## Why

A repo can be declared by clone URL, but only a public one works. For a private repo git asks for a
username on a terminal that does not exist, so the read hangs until a timeout and then fails with
`exit status 128`, which tells nobody what to fix. Teams mostly have private repos, so this is the
common case, not an edge.

## What Changes

- **Credentials in config, secrets outside it.** `git_credentials` maps a host to a username and the
  *name* of an environment variable holding the token; the token itself never sits in the config
  file. Tokens are sent only over HTTPS, as a request header, never in a URL or on a command line.
- **SSH.** `git_ssh_key` (and optional `git_known_hosts`) for `ssh://` and `git@` remotes.
- **Never wait for a person.** Every git call that touches the network runs with prompts disabled, so
  a missing credential fails at once with a message that says which host and what to configure.
- **No secret in any message or API answer.** Failures are reported with the host, never the token, and
  a URL carrying a password is refused at startup.
- The Docker image gets an SSH client and compose takes secrets from an optional env file.

## Capabilities

### Modified Capabilities
- `team-server`: repos declared by clone URL can be private.

## Impact

- **Code**: `internal/aggregate` (credential environment, non-interactive git, clearer failures),
  `internal/server` (config), `cmd/relai-server`, `Dockerfile`, `docker-compose.yml`.
- **Out of scope**: OAuth/GitHub App flows, credential helpers, token refresh.
