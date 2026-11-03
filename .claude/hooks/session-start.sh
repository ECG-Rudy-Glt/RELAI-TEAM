#!/bin/sh
# Runs when a Claude Code session starts (see .claude/settings.json). Whatever it prints becomes part of
# the session's context, so the agent starts from the project's current state instead of guessing.
# It never fails: a broken hook must not block a session.
cd "${CLAUDE_PROJECT_DIR:-.}" 2>/dev/null || exit 0
if command -v relai >/dev/null 2>&1 && relai handoff 2>/dev/null; then
  exit 0
fi
echo "# Handoff (the relai CLI is not installed, so this is read from the plain files)"
for f in .relai/notes.md relai.yaml; do
  if [ -f "$f" ]; then echo; echo "## $f"; cat "$f"; fi
done
exit 0
