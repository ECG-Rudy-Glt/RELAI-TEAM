## ADDED Requirements

### Requirement: The board can be fed from a folder
The server SHALL, when configured with a folder, add the git repositories found in it (symbolic links to repositories included, each once) to the board, look again at every refresh, and keep the items and claims of a repository that disappears.

#### Scenario: A project is cloned into the folder
- **WHEN** a repository is cloned into a discovered folder while the server runs
- **THEN** it appears on the board at the next refresh without any configuration change

### Requirement: The links between repos are shown, and mentions are kept apart
The server SHALL report what each repo's `relai.yaml` says it provides, consumes and works with, SHALL derive edges only from what repos declare, and SHALL return the mentions between repos found in their files separately, marked as not verified, never as links.

#### Scenario: Declared and capability edges
- **WHEN** one repo declares `links: [web-shop]` and consumes a capability another provides
- **THEN** the links view shows a declared edge to web-shop and a capability edge to the provider

#### Scenario: Mentions are evidence
- **WHEN** candidates are requested and one repo names another in its CI files
- **THEN** the mention is listed with its evidence counts, `declared: false`, and a note that it is not a verified link

### Requirement: Asking what already exists
The server SHALL answer which repos provide or consume a capability (case-insensitive) and who they work with, and its answer when nothing is found SHALL say that a repo may have the capability without having declared it.

#### Scenario: Before building
- **WHEN** an agent asks for a capability that a repo provides
- **THEN** it is told which repo provides it and which consume it
