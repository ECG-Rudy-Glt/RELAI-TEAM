## ADDED Requirements

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
