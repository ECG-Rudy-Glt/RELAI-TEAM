# Tasks

## 1. Board

- [x] 1.1 Add a lease and an injectable clock to the board: effective holder computed on read,
      `expires_at` on held items, claiming one's own item renews (new event), another member can claim a
      lapsed item; verify with tests using a fake clock for: lapse frees the item, renewal moves the
      lapse, late release by the old holder refused, restart replay keeps live/lapsed correct, lease
      zero never lapses.
- [x] 1.2 Add force release with the admin recorded in the event (`by`); verify it frees the item,
      survives replay, and that the history carries both identities.

## 2. Server and MCP

- [x] 2.1 Config `claim_lease` (default 8h, `0s` off) and member `admin`; `release?force=true`
      (403 for non-admin, 409 without force); expiry reaches clients through the change notifications;
      verify the three force scenarios over HTTP and that expiry notifies a subscriber.
- [x] 2.2 `relai-mcp`: `board_release` `force`, the lapse time shown for your own items, tool text says
      claiming again renews; web board shows when a claim lapses; verify with tests.
- [x] 2.3 Real run: a claim with a 3 s lease lapses on a real server, another member takes it over
      through `relai-mcp`, an admin force-release works through the MCP tool and the log names both
      (stdio sessions with the real binaries; real Claude agents were already run on the MCP tools).

## 3. Docs

- [x] 3.1 README (config, tools, "Know before you deploy" loses the no-expiry limit) and notes.

## Workflow follow-up

- Archive once reviewed.
