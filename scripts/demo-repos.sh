#!/usr/bin/env bash
# Create a few fictional projects to try relai-team on: git repos with Beads tasks, an OpenSpec
# change here and there, and a relai.yaml each. Nothing real is touched.
#
#   scripts/demo-repos.sh /tmp/demo
#   relai-server init --scan /tmp/demo --dir /tmp/demo-board
#   relai-server -config /tmp/demo-board/config.json      # then open /board
#
# Needs git and bd (Beads); openspec is optional (its changes are plain folders, but the server
# lists them through the openspec CLI). Each bd call rewrites its small database once: this is a
# one-time cost for a demo, not something the server does while running.
set -euo pipefail

ROOT=${1:?usage: demo-repos.sh <folder to create the demo repos in>}
command -v git >/dev/null || { echo "git is required" >&2; exit 1; }
command -v bd  >/dev/null || { echo "bd (Beads) is required: see https://github.com/steveyegge/beads" >&2; exit 1; }
export GIT_AUTHOR_NAME="Demo" GIT_AUTHOR_EMAIL="demo@example.test"
export GIT_COMMITTER_NAME="Demo" GIT_COMMITTER_EMAIL="demo@example.test"

mkdir -p "$ROOT"
ROOT=$(cd "$ROOT" && pwd)

# repo <name> <summary>
repo() {
  local dir="$ROOT/$1"
  if [ -e "$dir" ]; then echo "$dir already exists, leaving it alone" >&2; return 1; fi
  mkdir -p "$dir"
  cd "$dir"
  git init -q -b main
  printf 'apiVersion: relai.dev/v0\nid: %s\nsummary: "%s"\nowners: [demo-team]\nprovides: []\nconsumes: []\n' "$1" "$2" > relai.yaml
  printf '# %s\n\n%s\n' "$1" "$2" > README.md
  bd init --skip-agents --skip-hooks -q >/dev/null 2>&1 || true
}

# task <title> <priority> [status]   (status: in_progress | blocked)
task() {
  local id
  id=$(bd create "$1" -p "$2" --silent 2>/dev/null | tail -1)
  case "${3:-}" in in_progress|blocked) bd update "$id" -s "$3" >/dev/null 2>&1 ;; esac
}

# change <name> <why> <task...>   (prefix a task with "x:" for done)
change() {
  local name=$1 why=$2; shift 2
  local d="openspec/changes/$name"
  mkdir -p "$d/specs"
  printf 'schema: spec-driven\ncreated: 2026-11-03\n' > "$d/.openspec.yaml"
  printf '# Proposal\n\n## Why\n\n%s\n' "$why" > "$d/proposal.md"
  { echo "# Tasks"; echo; echo "## 1. Work"; echo; local n=1
    for t in "$@"; do case $t in x:*) echo "- [x] 1.$n ${t#x:}";; *) echo "- [ ] 1.$n $t";; esac; n=$((n+1)); done; } > "$d/tasks.md"
}

finish() { git add -A >/dev/null 2>&1; git commit -q -m "chore: demo project"; }

if repo auth-service "Sign-in and tokens for every internal service."; then
  task "Rotate signing keys without downtime" 1 in_progress
  task "Rate-limit the /token endpoint" 1
  task "Move the session store to Redis" 2 blocked
  task "Audit log for admin actions" 3
  change mfa-recovery-codes "People locked out of MFA need a way back in." \
    "x:Generate and hash recovery codes" "Show the codes once at enrolment" "Accept a code at login"
  finish
fi
if repo billing-api "Invoices, payments and tax."; then
  task "EU VAT calculation" 0 in_progress
  task "Handle partial refunds" 1
  task "Idempotency keys on POST /invoices" 1
  task "Webhook retries with backoff" 2
  finish
fi
if repo web-frontend "The customer-facing web app."; then
  task "Checkout form validation" 1 in_progress
  task "Lazy-load the dashboard charts" 2
  task "Spanish translation" 3
  task "Replace moment.js with date-fns" 3 blocked
  finish
fi
if repo mobile-app "iOS and Android apps."; then
  task "Crash on cold start (Android 14)" 0 in_progress
  task "Biometric login" 1
  task "Push notification opt-in screen" 2 blocked
  change offline-invoices "Field staff lose coverage and still need their invoices." \
    "x:Local cache schema" "Sync queue" "Conflict policy" "Settings toggle"
  finish
fi
if repo data-pipeline "Nightly batches and the warehouse."; then
  task "Backfill October events" 2 in_progress
  task "Dedupe step is quadratic on large batches" 1
  task "Schema registry integration" 2 blocked
  finish
fi
if repo design-system "Shared UI components."; then
  task "Dark mode tokens" 2
  task "Button focus ring contrast" 1
  task "Publish the v3 migration guide" 2 in_progress
  finish
fi

echo "Created demo projects in $ROOT:"
ls -1 "$ROOT"
echo
echo "Next:"
echo "  relai-server init --scan $ROOT --dir ${ROOT}-board"
echo "  relai-server -config ${ROOT}-board/config.json"
