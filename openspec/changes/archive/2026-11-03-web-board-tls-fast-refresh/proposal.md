# Proposal

## Why

Three gaps stop a team from actually relying on the board:

- tokens travel in clear over plain HTTP;
- a change in a repo takes about a second **per repo** to show up, because repos are read one after
  another (each read spawns `bd` and `openspec`), so ten repos mean a ten-second cycle, and clients
  have to poll to notice anything;
- there is no way to look at the board without writing a client.

A fourth problem, found while verifying the others, is the urgent one: **every `bd` run rewrites its
Dolt database** (~2-3 MiB of journal, index and manifest, even with `--readonly`), and the server ran it
for every repo on every refresh. On ten repos that measured ~9.5 MB/s of disk writes (~790 GiB/day) from
an idle server, and heated an NVMe drive to 80 °C.

## What Changes

- **Stop polling `bd`.** A repo is re-read only when a fingerprint of its files changes (Beads'
  `.beads/last-touched`, which Beads rewrites on writes and never on reads, plus `relai.yaml` and
  `openspec/changes`), or after a safety interval (`full_refresh_interval`, default 6 h). Clone URLs
  are checked with `git ls-remote` and only cloned when their HEAD moved.
- **TLS built in.** Optional `tls_cert` / `tls_key` in the config serve HTTPS directly. Listening
  beyond loopback without TLS logs a clear warning. `relai-mcp` works over `https://`, accepts an
  extra CA for self-signed setups, and warns when it would send its token over plain HTTP to a
  non-loopback host.
- **Fast refresh.** Repos are read in parallel with a bounded number of workers. The server pushes
  a "something changed" event over Server-Sent Events, so clients stop polling and see a claim or a
  closed task right away.
- **Web board.** A kanban page embedded in the binary at `/board`. It holds no data: the person
  enters their token, and the page then uses the same authenticated API and event stream as any
  other client.

## Capabilities

### Modified Capabilities
- `team-server`: adds requirements for bounded-latency refresh, change notification, a web board and
  transport security.

## Impact

- **Code**: `internal/aggregate` (parallel `All`), `internal/server` (TLS, `/events`, `/board`,
  embedded page), `cmd/relai-server`, `internal/mcp` and `cmd/relai-mcp` (https, CA, warning).
- **Config**: new optional `tls_cert`, `tls_key`, `refresh_concurrency`. Existing configs keep working.
- **Docs**: README (HTTPS, web board, known limits updated).
- **Out of scope**: claim expiry, clone-URL credentials, an `fsnotify`-style watcher.
