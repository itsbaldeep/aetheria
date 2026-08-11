# FEEDBACK — human playtest observations

The human writes playtest notes here (never overwritten by the agent).
The agent reads this at the start of each loop iteration, folds items into the
plan (human feedback outranks the default task order), and appends
`-> resolved:` notes under items it has addressed.

Expected human playtests: end of M2, M3, M5, M7, M10.

## M0 (no playtest expected — infrastructure milestone)
(empty)

---

## How to add feedback
Append a dated block: `## 2026-MM-DD — playtest <milestone>` followed by
`- ` bullet lines. A dated section containing unresolved `- ` bullets pauses
the autonomous loop until you address them.
