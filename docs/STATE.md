# STATE — Aetheria living status file

Update at the end of every work block. Read at the start of every session.

## Current milestone
**M0 — Foundations** (monorepo scaffold, toolchain, CI, deploy skeleton)

## Milestone checklists

### M0 — Foundations
- [x] Toolchain installed (Go 1.26.5, Godot 4.7.1, protoc 29.3, goose, protoc-gen-go)
- [x] Private GitHub repo created: github.com/itsbaldeep/aetheria
- [x] Project registered in ledger (orch project aetheria, id=29)
- [x] Ports allocated + ADR-001 written
- [x] docs/BRIEF.md committed
- [x] AGENTS.md, STATE.md, CHANGELOG.md, FEEDBACK.md, HUMAN_TODO.md written
- [x] Monorepo directory scaffold + .gitignore + .env.example
- [x] Postgres + Redis containers provisioned (net-aetheria)
- [x] Proto toolchain + first .proto (handshake/ping) + Go/Godot codegen
- [x] Empty Go services boot + health endpoints
- [x] Godot project boots to empty scene + headless test runner
- [x] Caddy route files + deploy.sh + backup.sh + migrations wiring
- [x] CI: make test + gofmt + go vet + contentlint + git hooks + GH Actions
- [x] botclient connects + protobuf ping/pong
- [x] Autonomous loop driver + crontab (job 12, every 30m)
- [x] Initial commit + push; make test green; all 4 services healthz OK
- [x] Client exports (Linux + Windows) verified running
- [ ] **YELLOW GATE — human approval:** 8 approvals pending (4 dns + 4 deploy)
      for aetheria/api/admin/play.apps.deployden.tech (ids 50–57).
      Once approved, executor writes Caddy routes; verify TLS on each.
- [ ] M0 tag m0-complete after approvals + TLS verified

### M1 — Accounts & Auth
- [ ] portal registration (email+password, validation, rate limits)
- [ ] authserver login → session token (argon2id)
- [ ] character create/select endpoints
- [ ] client login + character select/create screens
- [ ] gameserver WS handshake session validation + bans
- [ ] bot scenario: register → login → create char → authed WS session

## Blockers
- **HUMAN:** approve approvals 50–57 (dashboard :5001 → Approvals, or
  `orch approval decide <id> approved`) so the public subdomains go live.

## Next action
Human approves the 8 staged approvals → executor publishes Caddy routes →
verify TLS on all 4 subdomains → tag m0-complete → begin M1.

## Ports (ADR-001)
auth=3016 game=3015 admin=3017 portal=3018 control=5003 pg=5004 redis=5005

## Last session log
- 2026-08-11: M0 largely complete. make test green; 4 services running as
  Docker containers (ADR-003); client exports Linux+Windows; botclient
  ping/pong through containerized gameserver. 8 public approvals staged.
- 2026-08-11: loop driver live (job 12, every 30 min, 25 min budget/iter).
