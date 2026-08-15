# AETHERIA × AGENCY OS — Loop Automation & Dashboard Visibility Spec
### Addendum to docs/BRIEF.md. Commit as docs/AGENCY_INTEGRATION.md in the aetheria repo.
### Goal: retire manual OpenCode web sessions. The loop runs as Agency OS worker tasks; the human sees everything (agent activity, spend, human-action items, milestones, screenshots) in agency-dashboard, and steers via FEEDBACK.md + dashboard approvals.

---

## 0. Current state (verified facts — do not rediscover)

- Aetheria already lives on the Agency OS box: `~/projects/aetheria`, subdomains under `apps.deployden.tech` via caddy-apps + approval executor, ports from the ledger range (auth=3016 game=3015 admin=3017 portal=3018 control=5003 pg=5004 redis=5005).
- Agency OS primitives available: `orch` CLI (project/approval/dns/deploy/trace), Postgres ledger (`projects`, `services`, `tasks` w/ progress+cost, `approvals`, `background_jobs`, `job_runs`), ClickHouse `default.events` trace (cols: project, session_id, actor, action, detail, exit_code, duration_ms, model, tokens_in, tokens_out, cost_usd, gate, decision, ok), worker.py (polls `tasks`, runs `opencode run --dir <wt> --format json`, parses `step_finish` tokens for cost, `set_task_progress`), Discord webhook, dashboard on :5001 (projects, tasks + live task monitor, operations/approvals, spend, health).
- Job 12 ("loop driver") exists but never fired: the `.manual` marker guard was permanently set by the long-lived manual session. Also note `run-job.sh` runs scripts with `timeout 300` and sweeps runs stale after 30 min — so a work block can NEVER run inline in a job script.

---

## 1. Architecture: enqueuer job + worker task type

```
cron → run-job.sh 12 → aetheria-loop.sh (thin, <5 s)          [respects guards]
                          └── INSERT INTO tasks (type='aetheria_work_block')
worker.py → handle_aetheria_work_block(task)                   [the real block, ≤40 min]
                          └── headless `opencode run` session-per-task
dashboard → tasks page + task monitor + approvals + spend      [visibility]
```

### 1.1 `scripts/jobs/aetheria-loop.sh` (job 12's script — replace)
Enqueue-only, exits in seconds (fits the 300 s cap). Skip (exit 0, logging why) when ANY guard trips:
- `~/projects/aetheria/.manual` exists (human is driving a manual session — keep this convention);
- a task of type `aetheria_work_block` is already queued/running;
- an approval of type `aetheria_gate` is pending (human gate unanswered — see §3);
- daily budget exhausted: sum of today's `tasks.cost` for project aetheria ≥ `AETHERIA_DAILY_BUDGET_USD` (env, default 3.00);
- STATE.md's "Next action" line starts with `HUMAN:` (agent itself parked the loop).
Otherwise: `INSERT INTO tasks (type, params) VALUES ('aetheria_work_block', '{"repo":"aetheria"}')` and exit. Schedule: hourly.

### 1.2 `worker.py: handle_aetheria_work_block` (new handler)
Runs ONE work block, session-per-task (this replaces the auto-compacting eternal session):

1. `cd ~/projects/aetheria`; `git pull --ff-only`. No worktree needed (single writer; direct-to-main per BRIEF §12.5 discipline, small commits).
2. Compose the block prompt (see §2) and run `opencode run --dir ~/projects/aetheria "<prompt>" --format json --model $AETHERIA_MODEL` with timeout from params (default 2400 s). Parse `step_finish` tokens exactly like `handle_propose_fix`; accumulate cost with GLM-5.2 pricing ($1.40/M in, $4.40/M out) added to `MODEL_PRICING`.
3. `set_task_progress` at each stage: 5 pull, 10 opencode started, 70 opencode done, 80 verifying, 100 done. Put the block's one-line goal (read from STATE.md "Next action") in `progress_text` so the dashboard task list is self-describing.
4. **Trust but verify** (the worker, not the model, is the referee): after opencode exits, run `make test` (and `make bottest` if the block touched `server/`); if the block touched `client/`, run `make screenshots`. Red verification ⇒ task result `ok:false`, no push, `git reset --hard origin/main`, and trace the failure — the next block's prompt will include it.
5. On green: `git push`, then `orch trace` two events (`actor=agent`): `work_block_done` (detail = commit range + STATE.md's new "Next action", tokens_in/out, cost_usd, duration_ms, model) and, if screenshots were produced, `screens_published` (detail = gallery URL + sha).
6. Post Discord one-liner: `aetheria ▸ <block goal> ▸ green|red ▸ $<cost> ▸ <commit range>`.
7. Model routing per params: default `$AETHERIA_MODEL` = the cheap/free model; the enqueuer sets `"model": "<paid GLM-5.2>"` when STATE.md's next action is tagged `[UI]`, `[shader]`, `[arch]`, or the previous block on the same goal failed (escalation rule from M5.5 §6). Use `WORKER_ZEN_KEY` so aetheria spend is separable on the Zen dashboard.

### 1.3 The `.manual` contract (fix the old deadlock)
`.manual` is created by the HUMAN when opening a manual session and removed by the HUMAN when closing it. The agent never creates it. Belt-and-braces: the enqueuer ignores `.manual` if older than 24 h (`find -mmin +1440`) and traces `manual_marker_expired` — no more permanent lockout.

---

## 2. The work-block prompt (what each headless session receives)

Stored at `~/projects/aetheria/docs/BLOCK_PROMPT.md`; the handler injects the failure context of the previous red block if any. Content:

```
You are the Aetheria dev agent running ONE autonomous work block (no human present).
1. Read AGENTS.md, docs/STATE.md, and the current milestone doc listed there
   (now: docs/M5_5_UI_UX_OVERHAUL.md). Read FEEDBACK.md and fold in any new,
   unresolved human feedback FIRST — it outranks the default task order.
2. Do exactly ONE unchecked checklist item (or one FEEDBACK fix). Small commits,
   conventional messages. Tests land with code. Never weaken a test or a guardrail.
3. Verify yourself: make test; make bottest if you touched server/; make screenshots
   if you touched client/ (the worker re-verifies — a red result discards your work).
4. If your item needs a human (asset, review, approval, decision): do NOT stall.
   Stage it per docs/AGENCY_INTEGRATION.md §3, set STATE.md "Next action" to the
   next non-blocked item (or "HUMAN: <what>" if everything is blocked), and finish.
5. End of block: update docs/STATE.md (done / next / blockers), append one line to
   docs/CHANGELOG.md, commit. Your final message: one line — what you did, test
   status, what's next. Do not start a second item.
```

---

## 3. Human-in-the-loop via dashboard approvals (replaces "what do I need to do?")

Use the existing `approvals` table + Operations tab as the single human inbox. The agent stages these via `orch approval request --project aetheria --type <t> --payload <json>`:

| type | when | payload |
|---|---|---|
| `aetheria_gate` | milestone sign-off gates (m5.5 §5.6, playtest gates M7/M10) | `{"gate":"m5.5","gallery":"<url>","build":"<download url>","ask":"play + verdict in FEEDBACK.md"}` |
| `aetheria_screens` | each screenshot set needing review | `{"sha":"<sha>","gallery":"<url>","changed":"login, HUD"}` |
| `aetheria_human_todo` | new HUMAN_TODO item (VRoid models, UI art, payment keys) | `{"item":"...","spec_ref":"M5_5 §2","suggested":"..."}` |

Rules: `aetheria_gate` pauses the loop (enqueuer guard §1.1); the other two do NOT pause it. Approving in the dashboard = acknowledged; the human's actual verdict/asset still lands in FEEDBACK.md / the repo, which the next block reads. Rejecting = agent re-stages with the human's note folded in. This gives the dashboard's pending-approvals count the exact meaning "things Baldeep must do."

---

## 4. Dashboard visibility (changes to the agency-dashboard repo — do via its own dev-task/PR flow, it is a separate authorized repo)

Keep it minimal; reuse what renders already:

1. **Nothing needed for activity/spend/tasks** — once §1 lands, work blocks appear as tasks (with live monitor + progress), events show in project activity, and cost aggregates on the Spend tab. Verify, don't rebuild.
2. **Project detail page — "Game project" card** (renders only when the project has it): a `projects.meta` jsonb column (migration) holding `{"milestone":"M5.5","milestone_doc":"...","gallery_url":"...","download_url":"...","state_url":"...","feedback_hint":"..."}`, written by the worker at the end of each block (parsed from STATE.md header). Card shows: current milestone + checklist progress bar (checked/unchecked count parsed from STATE.md, stored in meta), buttons: Screenshot gallery · Client download · STATE.md · CHANGELOG (raw GitHub links are fine).
3. **Screenshot review inline**: on the approvals fragment, when payload has `gallery`, render it as a link button — one click from the inbox to the gallery. (The gallery itself stays on aetheria's admin server per M5.5 §1; don't duplicate it.)
4. Optional (only if cheap): a `milestone` gauge on the dashboard overview card for game-type projects.

Acceptance for §4: from the dashboard alone, with no OpenCode UI open, the human can answer — what did the agent do today and what did it cost (Spend + task list), is the loop green or stuck (task statuses + pending `aetheria_gate`), what do I owe it (Approvals), and where do I click to review screens / download the build (project card).

---

## 5. Rollout order (append these to STATE.md as the current task list)

1. Commit this doc + docs/M5_5_UI_UX_OVERHAUL.md; update AGENTS.md with the new standing rules (screenshot gate, session-per-task, §2 block contract).
2. Build M5.5 §1 screenshot pipeline first (it is a dependency of both the UI work and §3 review flow).
3. Implement §1.1 enqueuer + §1.2 worker handler + §1.3 marker expiry (agency-os repo — it is ledger-authorized; small PR, follow its conventions), register/repair job 12, add GLM-5.2 to MODEL_PRICING.
4. Stage the first `aetheria_screens` approval from the pipeline's maiden run — this validates the whole §3 loop end-to-end.
5. Dashboard §4 changes as a dev-task PR on agency-dashboard.
6. Remove `.manual`, enable job 12, watch two unattended blocks complete green from the dashboard — then the manual web session is officially retired for routine work.
```
