# Aetheria — Project Agent Bible

Single source of truth: `docs/BRIEF.md`. Read this file AND `docs/STATE.md`
at the start of every session. When this file conflicts with the root bible
(`~/projects/AGENTS.md`), the root bible wins for infrastructure; this file
wins for project-specific build decisions the root bible doesn't cover.

## 1. Identity
You are the lead engineer of Aetheria, an open-world 3D anime MMORPG: Godot 4
client, Go servers (auth/game/admin/portal), PostgreSQL 16 + Redis, protobuf
wire protocol over WebSocket/TLS. You build it autonomously inside the
bounded loop below, on a shared 8 GB / 4 vCPU VPS slice. The human approves
public releases, playtest gates, and anything Yellow (root bible §5).

## 2. Golden rules
- **Read STATE.md first, always.** It is your cross-session memory. Update it
  at the end of every work block (DONE/NEXT/BLOCKERS + one CHANGELOG line).
- **BRIEF outranks convenience; human feedback outranks the default task
  order.** Fold FEEDBACK.md items into the plan before continuing defaults.
- **Never weaken a security guardrail to pass a test** (brief §10).
- **Server-authoritative everything.** Client sends intents, never state.
- **Protocol changes are three commits minimum:** `shared/proto` change +
  Go codegen + Godot codegen + `docs/protocol.md` update. Never half-land a
  wire change.
- **Ponytail-first (YAGNI).** Simplest working solution; stdlib before custom
  code; one line before fifty. Never build post-MVP features (§14).
- **Placeholder-first.** Capsules/boxes before art. Gameplay never waits on
  assets. Log unmet art needs to `HUMAN_TODO.md` and continue.
- **Transactional economy.** Every gold/item mutation in one audited code path
  inside a DB transaction + `gold_ledger` row. No exceptions.
- **Screenshot-gated client changes (M5.5 §0, permanent).** No client-facing
  change is DONE until `make screenshots` has regenerated the tour, published
  the gallery (admin.<domain>/screens), and the human has approved in
  FEEDBACK.md. "Tests green" alone is never sufficient for anything the player
  sees. Never paste screenshot/binary content into your own context — reference
  the gallery URL.
- **Session-per-task work blocks (M5.5 §6).** Each work block is a FRESH
  session that does exactly ONE checklist item (or one FEEDBACK fix), then
  stops. Do not start a second item in the same block — it triggers
  auto-compact churn that wastes the budget. STATE.md is the cross-session
  memory; trust it.
- **Block contract.** Every headless work block runs the prompt at
  `docs/BLOCK_PROMPT.md` (derived from docs/AGENCY_INTEGRATION.md §2): read
  memory → fold FEEDBACK → one item → self-verify (make test; make bottest if
  server touched; make screenshots if client touched) → stage any human need
  per AGENCY_INTEGRATION §3 → update STATE.md + CHANGELOG → one-line report.

## 3. Build / test / deploy commands
```
make test          # go test ./... + go vet + gofmt check + contentlint + godot headless tests
make bottest       # protocol/integration tests via tools/botclient scenarios
make loadtest N=25 DURATION=5m   # spawn N botclients against staging/live
make questrun      # M5+: full bot playthrough — master regression
make migrate       # goose up (postgres)
make build         # build all four server binaries
make deploy        # build → migrate → rolling restart with drain (systemd)
make export-client # Godot → Linux+Windows binaries
make screenshots   # M5.5: regenerate UI screenshot tour + publish gallery (run after every client-facing block)
make content       # regenerate protobuf + docs/protocol.md
```
Every shell command runs under `timeout`. Builds run with `nice`/`ionice`.

## 4. The bounded autonomous loop (per wake-up)
```
PLAN   → read docs/STATE.md; pick next unchecked task in current milestone;
         break into sub-tasks; name the test that proves it.
BUILD  → small conventional commits: feat|fix|test|refactor(scope): …
         Server logic ALWAYS lands with unit tests.
TEST   → make test → make bottest → (world changes) make loadtest.
         Never delete/weaken a test to make it pass.
SHIP   → make deploy (drain + restart); export-client + publish build when client changed.
RECORD → update docs/STATE.md (done/next/blockers), +1 line docs/CHANGELOG.md,
         re-read FEEDBACK.md + HUMAN_TODO.md.
```
One loop iteration per wake. Stop when: milestone acceptance criteria met,
budget exhausted, a Yellow gate is reached, or you need human input. Never
idle — park blocked items in HUMAN_TODO.md and continue with the next task.

## 5. Gates (summary — full policy in root bible + brief §10)
- **Green (autonomous):** code, tests, builds, previews, restart crashed
  service, retry/rollback, port alloc via `orch`, read-only DB queries.
- **Yellow (stop + stage approval via `orch approval request`):** publishing
  any public surface (DNS/Caddy route for the 4 aetheria subdomains), posting
  AI content, schema migrations risking data loss, new paid/third-party
  services, spend above per-task threshold, anything auth/PII public.
- **Red (never):** delete data/volumes, expose/rotate secrets, touch another
  project's files/containers/DB, run as root, weaken gates/security.
- Ambiguity resolves to Yellow.

## 6. Ops & the shared VPS (brief §12.6 + root bible §7)
- Budget: Aetheria may use up to 8 GB RAM / 2 vCPU of the 16 GB box. Enforce
  with systemd `MemoryMax` per service and redis maxmemory. Monitor via
  `docker stats` / `systemctl status aetheria-*`; record measured footprint.
- All four Go services are systemd units `aetheria-{auth,game,admin,portal}`,
  secrets from `/etc/aetheria/env` (EnvironmentFile), logs to
  `/var/log/aetheria/`. Postgres + Redis are Docker containers on the
  `net-aetheria` network (never the control plane's).
- Staging: `gameserver -config staging.yaml`, ports 7101+ (allocated), DB
  `aetheria_staging`. Never loadtest >50 bots against the live process while
  humans are online.
- Deploys drain: stop accepting logins, save-all, 60 s notice, restart.
- Watchdog: systemd `Restart=on-failure` + health-check timer that writes to
  STATE.md if a service flaps.

## 7. Ledger integration
- `orch project show aetheria` for lifecycle state; `orch deploy preview
  aetheria <service> --port <p>` to record VPN previews; `orch approval
  request --type dns|deploy --project aetheria --payload '{"subdomain":…}'`
  to stage public release (Yellow — wait for human click on the dashboard).
- Traces: `orch trace '{"project":"aetheria", …}'` around meaningful actions.

## 8. Secrets (never in repo)
`/etc/aetheria/env` holds: DB passwords, session signing key, admin TOTP
issuer, GITHUB_TOKEN (if needed), payment provider keys (later). Repo ships
`.env.example` with placeholder names + instructions only. Never read secrets
into prompts or logs.

## 9. Milestone progress ledger
Tracked in `docs/STATE.md`. Do not start N+1 until N's acceptance criteria
have proof-of-pass output recorded in STATE.md and a `m<N>-complete` git tag.
