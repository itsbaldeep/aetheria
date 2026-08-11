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
- [ ] Monorepo directory scaffold + .gitignore + .env.example
- [ ] Postgres + Redis containers provisioned (net-aetheria)
- [ ] Proto toolchain + first .proto (handshake/ping) + Go/Godot codegen
- [ ] Empty Go services boot + health endpoints
- [ ] Godot project boots to empty scene + headless test runner
- [ ] Caddy routes + systemd units + deploy.sh + backup.sh + migrations
- [ ] CI: make test + gofmt + go vet + contentlint + git hooks
- [ ] botclient connects + protobuf ping/pong
- [ ] Autonomous loop driver + crontab registration
- [ ] Initial commit + push + M0 acceptance verification

### M1 — Accounts & Auth
(not started)

## Blockers
None.

## Next action
Finish M0 scaffold: directory layout, Makefile, env files, DB containers.

## Ports (ADR-001)
auth=3016 game=3015 admin=3017 portal=3018 control=5003 pg=5004 redis=5005

## Last session log
- 2026-08-11: Kickoff. All 6 human decisions locked (repo/domain/loop/budget/email/art).
  Toolchain installed user-local, repo + ledger registered, ADRs written.
