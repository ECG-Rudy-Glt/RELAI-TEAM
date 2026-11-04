# Design

- **Cursor = the journal's `seq`.** It already exists, is gap-free and survives a restart, so no new
  state is stored. Lease expiry is computed at read time and has no event: a lapsed claim does not
  appear in the changes. Clients that care re-list from time to time (stated in the tool text).
- **History in memory, 2,000 events.** Rebuilt on start from the journal. Beyond it the answer is
  `complete: false` instead of a partial list that could be mistaken for everything.
- **Same visibility as the other endpoints.** Events of repos the server does not show are left out.
- **Not a replacement for `/events`.** `/events` still only says "something changed"; a client can
  wake on it and then ask `/changes`.
