## ADDED Requirements

### Requirement: Clients can ask what changed since a sequence number
The server SHALL give every board event a sequence number, SHALL answer a request for the changes
after a given number with the current number and those changes in order, limited to the repos it
shows, and SHALL say when the history it keeps no longer reaches back to the requested number.

#### Scenario: Nothing happened
- **WHEN** a client asks for the changes since the current sequence number
- **THEN** it receives the same number and no changes

#### Scenario: Others claimed and released items
- **WHEN** other members claim and release items after the client's number
- **THEN** the client receives those events in order, each with who did it, and the new number

#### Scenario: The client was away too long
- **WHEN** the number is older than the history kept
- **THEN** the answer says it is not complete, so the client lists the board again instead of trusting a partial list

#### Scenario: Unauthenticated request
- **WHEN** the request carries no valid token
- **THEN** it is refused and no change is revealed
