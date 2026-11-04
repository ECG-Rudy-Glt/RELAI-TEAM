# Design

- **Read-only on purpose.** Writing back (assignee, labels) would need write scopes, conflict rules and a decision about
  which side is the truth. A mirror needs only an `issues: read` token and cannot damage the forge.
- **Polling cost.** The first page is sorted by last update, so any edit to an open issue changes it and its `ETag`.
  A closed older issue does not change page one: the full list is re-read every 6 hours as a safety net, the same
  idea as the repos' `full_refresh_interval`.
- **Token handling.** Environment variable only; one host; no redirect to another host; errors never carry the token.
- **Titles are untrusted.** Control characters stripped, length capped.
- **A mirror is a "repo".** It has an id, a source name (`github:owner/name`) and can be unreadable with a reason, so
  everything already built on repos (visibility, `/repos`, the web page, claims) applies unchanged.
