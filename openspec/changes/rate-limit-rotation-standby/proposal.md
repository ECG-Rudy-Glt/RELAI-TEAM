# Proposal

## Why

The README listed three limits: one instance, static tokens without rotation, no rate limiting. A review called them out. They
are not all the same kind: rate limiting and rotation are missing features; "one instance" is a design choice (state in memory,
one journal) whose real cost is that a crashed host stays down until a person restarts it.

## What Changes

- **Rate limits.** Wrong or missing tokens are capped per client address (default 10 a minute); past it the address gets `429` and
  its token is not looked at, so a token cannot be guessed by trying. Each member is capped (default 600 requests a minute).
  Both configurable, `0` turns one off, `trust_proxy` takes the address from the last `X-Forwarded-For` entry.
- **Rotation without downtime.** A member may have several valid token hashes (`token_sha256s`). `relai-server token` adds a new
  token (shown once) while the old one works; `-drop-old` removes the old ones. `SIGHUP` reloads members, tokens and limits
  without a restart and without losing a claim; a broken config is refused and the running one kept.
- **Failover.** `relai-server -standby` waits for the journal lock and takes over from the same journal when the active server
  stops or dies. Still one active server at a time.

## Impact

- Additive config keys and flags. Default limits are generous for normal use; a script that makes more than 600 requests a minute
  as one member now gets `429` with `Retry-After`.
