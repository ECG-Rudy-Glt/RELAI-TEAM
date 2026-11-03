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

### Requirement: Refresh time does not grow with the number of repos
The server SHALL read the declared repos concurrently, with a configurable upper bound on how many
are read at once, so that a refresh cycle takes about as long as the slowest repo rather than the
sum of all of them, while never reading the same repo twice at the same time.

#### Scenario: Many repos, one cycle
- **WHEN** ten declared repos each take a fixed time to read
- **THEN** a refresh cycle completes in far less than ten times that time

#### Scenario: One slow or broken repo
- **WHEN** one repo is unreadable or very slow
- **THEN** the others are still read and reported in the same cycle, and the broken one is reported
  as unreadable

### Requirement: Unchanged repos are not re-read
The server SHALL NOT run a repo's task tools again while the files they read from are unchanged, and
SHALL only re-read a repo when a cheap check (file sizes and times, or the remote's HEAD for a clone
URL) shows its inputs changed or when a configurable safety interval has passed. Reading a repo's
Beads state rewrites that tool's database on disk, so polling it on a timer causes continuous disk
writes even when nothing happens.

#### Scenario: Nothing changes for a long time
- **WHEN** no declared repo changes across many refresh cycles
- **THEN** no `bd` or `openspec` process is started after the first read, and no clone is made

#### Scenario: A task changes in a repo
- **WHEN** a Beads task is created, updated or closed, or an OpenSpec task is ticked
- **THEN** only that repo is re-read, on the next cycle

#### Scenario: A repo's task tool keeps failing
- **WHEN** reading a repo's Beads state fails
- **THEN** the failure is reported once and the tool is not started again every cycle until the repo's
  files change or the safety interval passes

#### Scenario: A change the check cannot see
- **WHEN** a repo's contents change in a way the cheap check does not detect
- **THEN** it is picked up no later than the safety interval

### Requirement: Clients are told when the board changes
The server SHALL let an authenticated client subscribe to change notifications, and SHALL send one
whenever an item is added, removed, changes status or holder, or a repo's readability changes, and
SHALL send none for a refresh that found nothing new. Notifications SHALL NOT carry board data.

#### Scenario: A claim by someone else
- **WHEN** one member claims an item while another is subscribed
- **THEN** the subscriber is notified without having to poll

#### Scenario: An unchanged refresh
- **WHEN** a refresh cycle finds exactly the state it already had
- **THEN** subscribers receive no change notification

#### Scenario: Unauthenticated subscription
- **WHEN** a client subscribes without a valid token
- **THEN** it is refused and receives nothing

### Requirement: A web board is available without exposing data
The server SHALL serve a web page showing the board as columns of work items with their holders,
which SHALL contain no board data itself and SHALL obtain data only through the authenticated API
using a token the person enters.

#### Scenario: Opening the page without a token
- **WHEN** someone opens the board page and has not entered a token
- **THEN** they see a prompt for a token and no repo names, item titles or holders

#### Scenario: Live update in the page
- **WHEN** the board changes while the page is open with a valid token
- **THEN** the page reflects the change without a manual reload

### Requirement: Transport can be secured
The server SHALL be able to serve HTTPS from a configured certificate and key, SHALL refuse a
configuration with only one of the two, and SHALL warn at startup when it listens on a non-loopback
address without TLS. The MCP adapter SHALL work over HTTPS, SHALL be able to trust an additional CA,
and SHALL warn on its error stream, never its protocol stream, before sending a token over plain
HTTP to a non-loopback host.

#### Scenario: HTTPS configured
- **WHEN** a certificate and key are configured
- **THEN** clients connect over HTTPS and the same API works unchanged

#### Scenario: Half a TLS configuration
- **WHEN** only one of certificate and key is configured
- **THEN** the server refuses to start and says which is missing

#### Scenario: Token over plain HTTP off-loopback
- **WHEN** the MCP adapter is pointed at an `http://` address that is not loopback
- **THEN** it prints a warning to stderr and still works

### Requirement: Claims lapse unless renewed
A claim SHALL last for a configurable lease measured from when it was made or last renewed, after
which the item SHALL count as unheld: it is shown without a holder and anyone may claim it. The holder
SHALL be able to renew by claiming the item again. A lease of zero SHALL mean claims never lapse.

#### Scenario: A holder disappears
- **WHEN** an agent claims an item and never renews it, and the lease passes
- **THEN** the item is shown as unclaimed and another member can claim it

#### Scenario: A holder keeps working
- **WHEN** the holder claims its own item again before the lease passes
- **THEN** it stays the holder and the lease restarts from that moment

#### Scenario: A lapsed holder acts late
- **WHEN** a holder whose claim lapsed releases the item, or the item was taken by someone else
- **THEN** the release is refused with the item's actual state, and nothing changes

#### Scenario: Restart
- **WHEN** the server restarts
- **THEN** claims that were live remain live until their lease ends and lapsed ones remain lapsed

#### Scenario: Leases disabled
- **WHEN** the lease is zero
- **THEN** a claim stays until its holder releases it

### Requirement: Administrators can force a release
A team member marked as administrator SHALL be able to release an item held by someone else, only by
asking explicitly. Anyone else asking SHALL be refused, an administrator not asking explicitly SHALL
be treated like any member, and the history SHALL record who forced the release.

#### Scenario: Admin frees a dead agent's task
- **WHEN** an administrator force-releases an item held by another member
- **THEN** the item becomes unheld and the history names both the former holder and the administrator

#### Scenario: A non-administrator tries to force
- **WHEN** a member who is not an administrator asks to force a release
- **THEN** it is refused and the holder is unchanged

#### Scenario: Admin without asking explicitly
- **WHEN** an administrator releases an item held by someone else without asking to force
- **THEN** it is refused like for any member

### Requirement: Private repos declared by clone URL can be read
The server SHALL read repos declared by clone URL that need authentication, using credentials the
operator configured per host (an access token supplied through the environment for HTTPS remotes, or
an SSH key), SHALL never wait for a person to type a credential, and SHALL NOT reveal any credential
in the config file, a remote URL, a process argument list, a log line, an error message or an API
answer.

#### Scenario: Private repo with a configured token
- **WHEN** a repo behind HTTPS authentication is declared and a token is configured for its host
- **THEN** it is read like any other repo

#### Scenario: No credential configured
- **WHEN** a private repo is declared and nothing is configured for its host
- **THEN** it fails at once, without waiting, and is reported as unreadable with a reason that names
  the host and says a credential is needed

#### Scenario: Wrong token
- **WHEN** the configured token is refused by the remote
- **THEN** the repo is reported as unreadable with an authentication reason, and the token does not
  appear anywhere in the reported reason

#### Scenario: Token over plain HTTP
- **WHEN** a repo on a plain `http://` remote has a matching credential configured
- **THEN** the credential is not sent and the repo is reported as unreadable, saying HTTPS is required

#### Scenario: Password embedded in a URL
- **WHEN** a declared repo URL contains a password
- **THEN** the server refuses to start and points to the credentials setting

#### Scenario: Missing token variable
- **WHEN** a configured token variable is empty or unset
- **THEN** the server refuses to start naming the variable
