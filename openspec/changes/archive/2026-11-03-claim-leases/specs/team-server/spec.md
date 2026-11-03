## ADDED Requirements

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
