# Tasks

- [x] `internal/mirror`: GitHub and GitLab readers, ETag, pagination cap, back-off, token scrubbing
- [x] Config `issue_mirrors` with validation (https, env var set, ids, interval)
- [x] Server: mirrors read in the refresh, listed as sources, unreadable on failure without losing items
- [x] Tests with fake forges; README, SECURITY, compose comment
- [ ] Try it against a real GitHub and a real GitLab project with a real token (not done: no token available here)
