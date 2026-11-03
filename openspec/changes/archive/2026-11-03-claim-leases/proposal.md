# Proposal

## Why

A claim never expires. If an agent crashes, loses its session or is simply closed while holding a
task, the task stays "taken" until that same identity releases it, and nobody else can. With agents
that start and die constantly this makes the board silently wrong: the work looks owned and is not.
There is also no way for a person in charge to free a task held by someone who is gone.

## What Changes

- **Leases.** A claim lasts `claim_lease` (default 8 h, `0s` = never). The holder renews it by
  claiming the item again. A claim past its lease counts as unheld: others can take the item, and
  clients see it as unclaimed.
- **Force release.** Members marked `admin` in the config can release an item someone else holds,
  by asking explicitly (`?force=true`). The log records who forced it.
- Items held show when their claim lapses (`expires_at`), and `relai-mcp` shows it to the agent that
  holds the item and tells it to renew.

## Capabilities

### Modified Capabilities
- `team-server`: claims are leased; admins can force a release.
- `mcp-team-interface`: `board_release` can force, claiming one's own item renews it.

## Impact

- **Code**: `internal/board` (lease, force, clock), `internal/store` (`by` on events),
  `internal/server` (config, admin, `force`), `internal/mcp`, the web board.
- **Compatibility**: existing logs replay unchanged; claims in them get the lease counted from when
  they were made. Existing configs get the 8 h default (set `claim_lease: "0s"` to keep claims forever).
