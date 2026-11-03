# mcp-team-interface Specification

## Purpose
Lets AI agents use the team board through MCP tools: see what work exists, claim it before
starting, and release it when done or handing off, as their own declared identity.

## Requirements

### Requirement: Board is exposed as MCP tools
The adapter SHALL speak MCP over stdio and SHALL expose tools to list repos, list work items
(optionally filtered by repo, status and holder), claim an item, release an item, and claim the
next available item.

#### Scenario: Agent discovers the tools
- **WHEN** an MCP client initializes the adapter and lists its tools
- **THEN** the adapter answers with the board tools and a description of each, without contacting
  the team server

#### Scenario: Agent lists unclaimed work
- **WHEN** an agent lists items filtered to unclaimed ones in one repo
- **THEN** it receives only items of that repo that have no holder, with their IDs usable as-is in
  claim and release calls

### Requirement: Calls act as the agent's own identity
The adapter SHALL authenticate every call to the team server with the token it was configured
with, SHALL NOT make any claim, release or visibility decision of its own, and SHALL NOT reveal
the token in any tool output.

#### Scenario: Claim is attributed to the caller
- **WHEN** an agent configured with member `agent-claude-1`'s token claims an item
- **THEN** the item's holder on the team server is `agent-claude-1`

#### Scenario: Unknown token
- **WHEN** the configured token is not a declared team member
- **THEN** every tool call reports an authentication failure and no board data is returned

### Requirement: Contention is a recoverable result
When a claim or release is refused because someone else holds the item, the adapter SHALL return
a tool result marked as an error that names the current holder, rather than a protocol error or a
crash.

#### Scenario: Lost race
- **WHEN** an agent claims an item another identity already holds
- **THEN** the result says the item is held and by whom, and the agent can continue with other work

#### Scenario: Releasing someone else's item
- **WHEN** an agent releases an item held by another identity
- **THEN** the result is refused, names the holder, and the holder is unchanged

### Requirement: Agents can claim the next available item
The adapter SHALL provide a single call that claims the first unclaimed, non-blocked item matching
optional filters, moving on to the next candidate whenever the server reports it was just taken.

#### Scenario: Several agents ask for work at once
- **WHEN** N agents call claim-next simultaneously and at least N matching items exist
- **THEN** each agent receives a different item

#### Scenario: Nothing available
- **WHEN** no unclaimed, non-blocked item matches
- **THEN** the result says so plainly and claims nothing

### Requirement: Server problems do not break the session
If the team server is unreachable or answers unexpectedly, the adapter SHALL return an error tool
result describing the problem and SHALL keep serving later calls.

#### Scenario: Server down, then back
- **WHEN** a tool call is made while the server is unreachable and a later call is made after it
  is back
- **THEN** the first call reports the server as unreachable and the second succeeds

### Requirement: Agents can see and renew their leases
The adapter SHALL show an agent when each claim it holds lapses, SHALL make clear that claiming an
item it already holds renews it, and SHALL let an administrator identity force-release an item held by
someone else when it asks to, refusing clearly for everyone else.

#### Scenario: Agent sees its own lease
- **WHEN** an agent lists items it holds
- **THEN** each shows when its claim lapses

#### Scenario: Agent renews
- **WHEN** an agent claims an item it already holds
- **THEN** the claim is renewed and the new lapse time is shown

#### Scenario: Forcing without being an administrator
- **WHEN** an agent that is not an administrator asks to force-release another member's item
- **THEN** the result is an error saying that forcing needs an administrator, and the holder is unchanged
