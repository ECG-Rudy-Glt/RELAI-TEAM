# Design

## Decisions

**Reads must not cause writes, so unchanged repos are not read.** Measured: one `bd list --json` rewrites
about 2-3 MiB of its Dolt database whatever the flags (`--readonly` included); `openspec` and `git`
write nothing. The Dolt files change on every run, so they cannot be the change signal. Beads'
`.beads/last-touched` is rewritten by every write (create, update, close, show) and never by a read
(checked), which makes it a usable signal. The fingerprint is a hash of sizes and modification times
of `relai.yaml`, `.beads/last-touched` and `openspec/changes` (archive excluded): a few `stat` calls, no
process. A clone URL is fingerprinted by `git ls-remote <url> HEAD`, which writes nothing, instead of
cloning to a temp directory each cycle. A repo whose reading failed is cached like any other, so a
broken `bd` is not retried in a loop. `full_refresh_interval` (default 6 h) is the safety net for a
change the fingerprint cannot see; `0s` disables the cache and is documented as the expensive mode.
Alternative rejected: lowering the polling frequency only, which still writes at a constant rate
for no reason.

**The slowness is process spawning and sequencing, not the network protocol.** Each repo read runs
`bd` and `openspec` (about half a second each); doing that for N repos one after another is what
makes the cycle N times longer. Reading repos concurrently, with a cap, cuts a ten-repo cycle to
roughly the time of the slowest repo. A repo is never read by two workers at once, so `bd`'s
per-repo lock files are not contended. The cap (`refresh_concurrency`, default 8) protects the host
from forking dozens of processes.

**Push over Server-Sent Events, not WebSocket.** The only server-to-client message is "something
changed, re-fetch". SSE is plain HTTP, works through proxies, needs no dependency, and is trivial to
consume from Go and the browser. Clients re-fetch `/items` on the event, so the event carries no
board data and cannot leak anything. The page uses `fetch` streaming rather than `EventSource` so it
can send the `Authorization` header.

**What counts as a change.** A digest of items (id, status, holder) and repo health is compared
after each refresh; claims and releases notify directly. Refreshes that find nothing new send
nothing, so idle teams see no traffic beyond keep-alives.

**The board page is public and empty; the data is not.** `/board` serves static HTML with no board
content. The user pastes their token, kept in `sessionStorage` (gone when the tab closes), and the
page calls the authenticated API. This avoids cookies, CSRF and a second auth path.

**TLS is optional and in-process.** Teams that already run a reverse proxy keep doing so; teams that
do not get HTTPS with two config lines and no extra component. Minimum TLS 1.2. Serving beyond
loopback without TLS logs a warning at startup rather than refusing, so private-network setups keep
working.

**`relai-mcp` never silently downgrades.** It uses whatever scheme it is given. For `http://` to a
non-loopback host it prints one warning to stderr (never stdout, which is the protocol stream).
`RELAI_TEAM_CA_FILE` adds a CA certificate for self-signed deployments.

## Risks / Trade-offs

- A page that accepts a pasted token is only as safe as the HTTPS it is served over: the README says
  to use TLS for anything beyond a private network.
- Parallel reads raise peak CPU and process count briefly each cycle. Bounded by the cap.
- Many open SSE connections hold goroutines; one per open board tab or agent, which is the point.
