## ADDED Requirements

### Requirement: Guessing a token is limited
The server SHALL refuse a client address that has sent too many wrong or missing tokens in a minute with `429` and a `Retry-After`, without looking at the token it presents, and SHALL cap the requests of one member per minute, with both limits configurable and switchable off.

#### Scenario: Too many wrong tokens
- **WHEN** one address sends more wrong tokens than the limit in a minute
- **THEN** its next requests, even with a correct token, get `429` until the budget refills

#### Scenario: Another member is not affected
- **WHEN** one member exceeds the request cap
- **THEN** only that member gets `429`

### Requirement: Tokens rotate without downtime
The server SHALL accept several tokens for one member at the same time and SHALL apply changes to members, tokens and limits on a reload signal without a restart and without losing any claim, keeping the running configuration if the new one is invalid.

#### Scenario: Rotation
- **WHEN** a new token is added for a member and the server reloads
- **THEN** both tokens work, and after the old one is removed and the server reloads only the new one works and the member's claims are unchanged

### Requirement: A standby takes over a dead server
The server SHALL, when started as a standby, wait for the journal lock held by another server and take over from the same journal when that server stops or dies, serving nothing before then.

#### Scenario: The active server dies
- **WHEN** the active server is killed
- **THEN** the standby takes over and the claims made before are still held
