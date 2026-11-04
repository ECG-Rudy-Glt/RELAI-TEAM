## ADDED Requirements

### Requirement: Issues of a GitHub or GitLab project can be shown as work items, read-only
The server SHALL, when configured with a GitHub or GitLab project and the name of an environment variable holding a
token, show the project's open issues as work items that can be claimed like any other, SHALL NOT write anything to
the forge, SHALL send the token only to the configured host over HTTPS, and SHALL NOT reveal the token in the config
file, a log line, an error message or an API answer.

#### Scenario: Open issues become items
- **WHEN** a project with open issues and pull requests is mirrored
- **THEN** each open issue is an item with its number in the id, pull requests are not, and a claim on an item is kept in the team's journal without touching the issue

#### Scenario: The forge is unreachable
- **WHEN** the forge cannot be reached or refuses the token
- **THEN** the items already shown stay, the source is reported unreadable with a reason, and the token does not appear in it

#### Scenario: Rate limit
- **WHEN** the forge answers that the rate limit is reached
- **THEN** the server stops asking until the limit is over

#### Scenario: Unsafe configuration
- **WHEN** the base URL is plain HTTP to a non-local host, contains credentials, or the token variable is empty
- **THEN** the server refuses to start and says which setting to fix
