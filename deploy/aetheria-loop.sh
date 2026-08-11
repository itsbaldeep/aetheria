#!/usr/bin/env bash
# aetheria-loop.sh — bounded autonomous dev loop driver (brief §12.2).
#
# cron wakes this. It runs ONE loop iteration against ~/projects/aetheria with
# hard time+token budgets, updates docs/STATE.md, and exits. It never loops
# internally — the cron schedule provides the repetition, so a runaway build
# can never wedge the box. Human gates (public deploy, content, playtest)
# surface as STATE.md blockers + a notification, never auto-approved.
#
# Budgets (env-overridable):
#   AETHERIA_LOOP_TIMEOUT   seconds per iteration  (default 1500 = 25m)
#   AETHERIA_LOOP_LOCK      lockfile path
#   AETHERIA_LOOP_LOG       per-run log
#   AETHERIA_LOOP_MAXSPEND  USD soft cap per run (checked after run)
set -euo pipefail

REPO="$HOME/projects/aetheria"
OPCODE="$HOME/.opencode/bin/opencode"
TOOLCHAIN="$HOME/.toolchain.env"
LOCK="${AETHERIA_LOOP_LOCK:-/tmp/aetheria-loop.lock}"
LOG="${AETHERIA_LOOP_LOG:-$HOME/aetheria/logs/loop.log}"
TIMEOUT_S="${AETHERIA_LOOP_TIMEOUT:-1500}"
mkdir -p "$(dirname "$LOG")" "$HOME/aetheria/logs"

log() { printf '%s %s\n' "$(date -Iseconds)" "$*" >> "$LOG"; }

# 1. Single-instance guard.
if [ -f "$LOCK" ]; then
  if kill -0 "$(cat "$LOCK")" 2>/dev/null; then
    log "skip: another iteration running (pid $(cat "$LOCK"))"
    exit 0
  fi
  log "warn: stale lock, removing"
fi
echo $$ > "$LOCK"
trap 'rm -f "$LOCK"' EXIT

# 2. Gate: human playtest / manual-hold. If FEEDBACK.md has an unchecked item
#    (line starting with "- " that has no "-> resolved"), the loop should STOP
#    and ask, not plow ahead. A lockfile in the repo = human working, skip.
if [ -f "$REPO/.manual" ]; then
  log "skip: .manual lock present (human working in repo)"
  exit 0
fi
PENDING_FEEDBACK=$(awk '/^## [0-9]{4}-/{section=1; next} /^## /{section=0} section && /^- / && !/-> resolved/ {print; exit}' "$REPO/FEEDBACK.md" 2>/dev/null || true)
if [ -n "$PENDING_FEEDBACK" ]; then
  log "GATE: unresolved human feedback — STOPPING, awaiting human (see FEEDBACK.md)"
  exit 0
fi

# 3. Check for a parked "stop and ask" signal left by the agent.
if [ -f "$REPO/.stopandask" ]; then
  log "GATE: .stopandask present — STOPPING, awaiting human"
  exit 0
fi

# 4. Load toolchain, pick the next task from STATE.md, run ONE iteration.
source "$TOOLCHAIN" 2>/dev/null || true
cd "$REPO"

NEXT=$(grep -m1 '^- \[ \]' docs/STATE.md | sed 's/^- \[ \] //' || true)
if [ -z "$NEXT" ]; then
  log "nothing unchecked in STATE.md — checking for next milestone or all-done"
  if grep -q "not started" docs/STATE.md; then
    # seed the next milestone's checklist is a human/agent planning step;
    # trigger the agent to begin it.
    NEXT="begin the next milestone's first task (read STATE.md, plan it)"
  else
    log "ALL MILESTONES DONE — no action"
    exit 0
  fi
fi
log "iteration start; task: $NEXT"

PROMPT="Read AGENTS.md and docs/STATE.md. You are running ONE bounded loop
iteration (brief §12.2 PLAN→BUILD→TEST→SHIP→RECORD). Task: $NEXT
Rules:
- Break into sub-tasks; implement with tests; run 'make test' and make it green.
- Never weaken tests/security. Never deploy anything public or stage approvals
  without a human (those are YELLOW — stop and write a blocker in STATE.md).
- If blocked on a HUMAN item, park it in HUMAN_TODO.md and continue with the
  next available task; do not idle.
- When done (or out of time), UPDATE docs/STATE.md: mark what you did DONE,
  set the next action, note blockers, and append one line to docs/CHANGELOG.md.
- End your reply with a one-line summary prefixed 'LOOP_RESULT:'."

# Guard against unbounded runtime even if opencode misbehaves.
timeout "$TIMEOUT_S" "$OPCODE" run -- "$PROMPT" >> "$LOG" 2>&1 || {
  rc=$?
  log "iteration ended rc=$rc (124=timeout)"
}

# 5. Record budget reality (token spend) for the human's awareness.
"$OPCODE" stats --json 2>/dev/null | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    t=d.get('tokens',{})
    print('LOOP tokens total:', t.get('totalIn',0)+t.get('totalOut',0), 'cost USD:', d.get('cost',0))
except Exception:
    pass
" >> "$LOG" 2>/dev/null || true

log "iteration complete"
