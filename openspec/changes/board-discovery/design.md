# Design

- **Evidence is separate from declaration.** Edges come only from what repos declare. Mentions are returned under
  `candidates`, flagged `declared: false`, with a note saying they are not verified. An agent is told to check the files and ask.
- **Mentions are opt-in and cached.** They run `git grep` between every pair of repos on the server's disk (read-only, no writes), which
  is too much for every request; 30-minute cache keyed by the set of repos. Repos read by clone URL are not searched.
- **Discovery is re-run at each refresh**, a directory listing and a few `stat` calls: cheap, and it makes "clone it there" enough.
- **A repo that disappears becomes unreadable, its items and claims stay**, as for any repo that cannot be read.
- **Not found is not "does not exist".** `board_find` says that a repo may have a capability without having declared it.
