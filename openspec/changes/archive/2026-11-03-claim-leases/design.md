# Design

## Decisions

**Expiry is computed when read, not scheduled.** A claim stores when it was made (already in the
log); the board treats it as live while `now < claimed_at + lease`. No timer, no background job, no
"expired" event to keep consistent, and a restart cannot lose or resurrect anything: replaying the
log gives the same answer because the time of each claim is in it. Changing `claim_lease` applies to
existing claims too, which is the intuitive behaviour for an operator.

**Renewing is claiming again.** The holder asking for the item it already holds appends a new claim
event, which moves `claimed_at` forward. No new endpoint or tool; the tool text says it. An agent
that works for hours renews as it goes; one that dies stops renewing and the item frees itself.

**Clients learn of expiry on the next refresh tick.** The change-detection digest already includes
holders, so an expiry is noticed by the periodic check (`refresh_interval`) and pushed over the
event stream. No extra timer is worth the complexity for a lease measured in hours.

**Force release is explicit and admin-only.** `?force=true` plus `admin: true` on the member.
Forcing is never implied: an admin releasing someone else's item without `force` is refused like
anyone. The released event keeps the old holder as `identity` and records the admin in `by`, so the
history says who freed what.

**Default 8 h.** Long enough for a working day of a person or a long agent task that renews, short
enough that a dead agent does not block a task past the day. `0s` keeps today's behaviour.

## Risks / Trade-offs

- A holder that forgets to renew loses the item mid-work. The tool output tells agents the lapse time;
  work already done is unaffected, only the "someone is on it" marker lapses.
- Two parties can briefly both think they own an item (the lapsed holder and the new one). The board
  only ever has one holder; the old holder's next call tells it so (its release is refused as "not held").
