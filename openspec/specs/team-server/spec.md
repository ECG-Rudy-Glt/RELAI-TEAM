# team-server Specification

## Purpose
Gives a team a live, shared view of work across the repos of one ecosystem, aggregated from each
repo's existing contract (`relai.yaml`, Beads tasks, OpenSpec changes) instead of a separately
maintained tool.

## Requirements

### Requirement: Server aggregates a declared set of repos
The team server SHALL maintain a live view built from the contract files
(`relai.yaml`, Beads tasks, OpenSpec changes) of every repo in a team-declared repo list, refreshed
without requiring a person to manually rebuild an index file.

#### Scenario: A declared repo's task changes
- **WHEN** a task's status changes in a repo the server is configured to track
- **THEN** the server's live view reflects the new status without a person re-running an index
  build command

#### Scenario: An undeclared repo is queried
- **WHEN** a client asks the server for a repo that is not in its declared repo list
- **THEN** the server reports that the repo is not tracked, rather than returning stale or
  fabricated data

### Requirement: Work items track a single holder
Each unit of work the board surfaces (a Beads task, an OpenSpec change, or a manually created item)
SHALL have at most one holder (a human or an agent identity) at a time, and the server SHALL expose
who currently holds it.

#### Scenario: Two agents attempt to claim the same item
- **WHEN** two different agent identities attempt to claim the same unclaimed work item at
  overlapping times
- **THEN** exactly one claim succeeds and the other is rejected with the current holder's identity

#### Scenario: Holder releases a work item
- **WHEN** the current holder releases or completes a work item
- **THEN** the item becomes claimable by another identity and the release is visible to all clients
  reading the board

### Requirement: Board state survives a server restart
The server SHALL persist board state (work items, holders, history) so that a restart does not lose
in-progress claims or history.

#### Scenario: Server restarts with in-progress claims
- **WHEN** the server process restarts while a work item is held by an identity
- **THEN** after restart the item is still shown as held by that same identity

### Requirement: Access is scoped to a declared team
The server SHALL require an identity (human or agent) to authenticate before reading or modifying
board state, and SHALL reject requests from identities not authorized for the declared team.

#### Scenario: Unauthenticated read
- **WHEN** a client reads board state without a valid identity
- **THEN** the server rejects the request rather than returning board data

#### Scenario: Authenticated but unauthorized identity
- **WHEN** a client with a valid identity that is not a member of the declared team requests board
  state
- **THEN** the server rejects the request
