## ADDED Requirements

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
