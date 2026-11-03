# Design

## Decisions

**Token via environment variable name, not value, in the config.** The config file is often
committed, mounted or shown; secrets belong in the environment or a secret store. The config says
`"token_env": "GITHUB_TOKEN"`, and the server refuses to start if that variable is empty, so a typo
is caught at once instead of surfacing as an unreadable repo later.

**Header, not URL.** Git is given `http.<url>.extraHeader` through `GIT_CONFIG_COUNT/KEY/VALUE`
environment variables. Unlike `https://user:token@host/…` this keeps the token out of the
`argv` (visible in `ps`), out of the remote URL stored in the clone, and out of git's own error text.
Scoped to the exact scheme and host, so it cannot leak to another server through a redirect.

**HTTPS only.** A matching credential is not sent over plain `http://`: that would put a token on
the wire in clear for no reason. The repo then fails with a message saying so.

**A URL that already contains a password is refused at startup**, because the declared source is
shown to every member in `/repos` and used as a key; the message points to `git_credentials`.

**Fail fast, say why.** `GIT_TERMINAL_PROMPT=0`, a non-interactive ssh (`BatchMode=yes`) and no
askpass mean git can never block on a person. Stderr is read, scrubbed of every known token, reduced
to its first line and mapped to plain hints (no credentials for host X, authentication refused,
host key unknown) before it becomes the repo's reported reason.

**SSH.** `git_ssh_key` becomes `ssh -i <key> -o IdentitiesOnly=yes -o BatchMode=yes`; with
`git_known_hosts` host keys are verified strictly against that file, otherwise first-seen keys are
accepted (`accept-new`), which the README says is weaker.

## Risks / Trade-offs

- A token in the process environment is readable by the same OS user; that is how every
  12-factor secret works and is stronger than a config file.
- Static tokens do not rotate; teams wanting short-lived tokens can refresh the variable and
  restart. A refresh mechanism is out of scope.
