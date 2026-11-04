## What and why
<!-- What changed, why, in two or three sentences. One concern per PR. -->

## How I verified it
<!-- Commands run and what they showed. Say what you did NOT verify. -->

## Checklist
- [ ] `gofmt -l .` prints nothing, `go vet ./...` and `go test -race ./...` pass
- [ ] A test comes with the change (a bug fix has a test that fails without it)
- [ ] The README, `docs/` and `openspec/specs/` still tell the truth
- [ ] A behaviour change started as an OpenSpec change (`openspec validate <name> --strict`), or it is a small fix
- [ ] Nothing here calls `bd` in a loop or on a timer; if the code that reads repos changed, I measured the writes of an idle server
- [ ] No secret, token or personal data in the diff, the tests or the logs
- [ ] Commits are conventional (`feat:`, `fix:`, `docs:`, `test:`, `chore:`), in English, with no `Co-Authored-By` or "Generated with" trailer
- [ ] Task list updated (`scripts/sync-beads.sh`, then commit `.beads/issues.jsonl`) when tasks changed
- [ ] If this touches the journal format, authentication, rate limits or the mirror: noted in `SECURITY.md` or the README limits
