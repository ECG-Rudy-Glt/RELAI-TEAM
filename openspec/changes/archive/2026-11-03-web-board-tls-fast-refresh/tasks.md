# Tasks

## 0. Stop the disk writes

- [x] 0.1 Fingerprint repos (local: `relai.yaml`, `.beads/last-touched`, `openspec/changes` minus
      archive; clone URL: `git ls-remote`) and skip unchanged ones until `full_refresh_interval`;
      verify with tests: zero `bd`/`openspec` runs over 50 unchanged cycles, re-read on a Beads write,
      an OpenSpec edit, a `relai.yaml` edit, after the safety interval, a failing `bd` not retried per
      cycle, clone made only when the remote HEAD moves, race-clean.
- [x] 0.2 Measure disk writes of an idle server on ten repos before and after (block-device counters
      over 30 s, baseline noise subtracted); record in `.relai/notes.md`.

## 1. Parallel refresh

- [x] 1.1 Make `Aggregator.All` read repos concurrently with a bounded worker count (config
      `refresh_concurrency`, default 8), preserving declared order in the result; verify with a test
      using a runner that sleeps per call (ten repos finish in well under the sequential time), one
      failing repo among healthy ones, and `-race`.
- [x] 1.2 Measure on the ten-repo fixture before and after; record both numbers in `.relai/notes.md`.

## 2. Change notifications

- [x] 2.1 Add a change broadcaster to the server: digest comparison after `Refresh`, direct notify on
      claim/release; `GET /events` (authenticated SSE with keep-alives); verify the three spec
      scenarios (claim notifies, unchanged refresh does not, no token is refused).

## 3. Web board

- [x] 3.1 Embed the kanban page and serve it at `GET /board` (public, no data); token entry in
      `sessionStorage`; live via `/events` with a polling fallback; verify the page body contains no
      repo/item data, and `/items` still needs a token.
- [x] 3.2 Look at it in a real browser against the ten-repo fixture and fix what is wrong.

## 4. TLS

- [x] 4.1 `tls_cert`/`tls_key` in config (both or neither), HTTPS serving, TLS 1.2 minimum, startup
      warning off-loopback without TLS; verify with a test using a generated self-signed certificate.
- [x] 4.2 `relai-mcp`: https works, `RELAI_TEAM_CA_FILE`, stderr warning for plain HTTP off-loopback;
      verify against an `httptest` TLS server and that stdout stays protocol-only.

## 5. Docs and release

- [x] 5.1 README: HTTPS, web board, parallel refresh and config; remove the limits this fixes.
- [x] 5.2 Run `relai-mcp` against the HTTPS server with a custom CA through a real stdio session
      (real-agent runs were already done for `mcp-team-interface`; the transport is the only change).

## Workflow follow-up

- Archive once reviewed.
