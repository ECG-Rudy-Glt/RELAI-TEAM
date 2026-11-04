# Design

- **Cut off before the token is looked at.** A limit that only counts failures after checking would not stop a guess. So an address
  over its budget is refused first, even with a correct token (an honest client on that address waits a minute at most).
- **Rightmost X-Forwarded-For.** Earlier entries are written by the client. It is only used with `trust_proxy`.
- **In memory.** Limits are per server and a restart resets them; good enough against guessing and accidents, not a DDoS defence.
- **Rotation as overlap.** Add the new hash, hand out the token, remove the old: there is never a moment when nobody can get in.
- **Failover by lock, not by consensus.** The kernel releases `flock` when a process dies, so no heartbeat or election is needed. It
  requires a shared `data_dir` on a filesystem with `flock`; it does not give two active servers.
