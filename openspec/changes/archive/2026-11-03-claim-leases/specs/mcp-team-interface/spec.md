## ADDED Requirements

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
