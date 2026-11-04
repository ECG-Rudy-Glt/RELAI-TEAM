# Proposal

## Why

Many projects keep their tasks as GitHub or GitLab issues, not Beads. Their work is invisible on the board, and
agents cannot claim it. The board only needs to reflect the current state of those issues; it does not need to
change them.

## What Changes

- New config `issue_mirrors`: a GitHub or GitLab project, the name of an environment variable holding a read-only
  token, and optionally an API base URL, a repo id and a minimum interval.
- Open issues appear as items (`<repo_id>/gh/<n>` or `<repo_id>/gl/<iid>`), with status from well-known labels.
- Read-only: nothing is ever written to the forge. Claims stay in the team's journal.
- Bounded polling: minimum interval, conditional requests, a cap of 300 issues, back-off on rate limits.

## Impact

- New optional config key and a new package; nothing existing changes. Docker already passes secrets by environment.
