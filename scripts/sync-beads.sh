#!/bin/sh
# Export the Beads tasks to .beads/issues.jsonl, the copy of the task list that lives in git.
#
# Why: Beads keeps its database outside git (.beads/embeddeddolt is ignored), so a fresh clone has
# no tasks until `bd bootstrap --yes` imports this file. Run this after creating, updating or
# closing tasks, and commit the result.
#
# The export's "owner" field is the creator's e-mail address. It is removed here so personal data
# is not published with the task list.
set -eu
cd "$(git rev-parse --show-toplevel)"
command -v bd >/dev/null || { echo "sync-beads: bd (Beads) is not installed" >&2; exit 1; }
bd export -o .beads/issues.jsonl >/dev/null
sed -E 's/"owner":"[^"]*",//' .beads/issues.jsonl > .beads/issues.jsonl.tmp
mv .beads/issues.jsonl.tmp .beads/issues.jsonl
echo "sync-beads: $(wc -l < .beads/issues.jsonl | tr -d ' ') tasks in .beads/issues.jsonl"
