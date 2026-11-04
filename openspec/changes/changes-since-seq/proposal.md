# Proposal

## Why

An agent that comes back to the board has to list every item again to learn what changed, even though
it already knew the board a few minutes ago. With dozens of items across several repos that is the
largest part of what it reads each time. The journal already numbers every event, so "what happened
since number N" is free to answer.

## What Changes

- The server keeps the most recent events in memory and answers `GET /changes?since=N` with the
  current sequence number and the changes after N, limited to the repos the server shows.
- When N is older than the history kept, or more than 200 changes happened, the answer says it is
  not complete, and the client lists the board once and starts again from the new number.
- `relai-mcp` gets a `board_changes` tool that returns those changes in a few lines, with the number
  to use next time.

## Impact

- New endpoint and tool; nothing existing changes. The history is rebuilt from the journal at start.
