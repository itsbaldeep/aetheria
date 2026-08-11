# STATE — Aetheria living status file

Update at the end of every work block. Read at the start of every session.

## Current milestone
**M1 — Accounts & Auth** (portal registration, authserver login, characters, session validation)

## Milestone checklists

### M0 — Foundations ✅ m0-complete (tag)
- [x] Toolchain, repo, ledger, ADRs, ports, docs
- [x] Postgres 16 + Redis containers; 22-table migration
- [x] Proto codegen (Go + GDScript) byte-identical; botclient ping/pong
- [x] 4 services as Docker containers, healthz green
- [x] Godot project boots; Linux + Windows exports verified
- [x] make test green; CI workflow; loop driver (job 12)
- [x] **ALL 4 SUBDOMAINS LIVE OVER TLS** (approved 2026-08-11):
      aetheria / api.aetheria / admin.aetheria / play.aetheria.apps.deployden.tech
      bot connects wss://play.aetheria... roundtrip 1ms
- [x] Tagged m0-complete

### M1 — Accounts & Auth (current)
- [ ] portal registration (email+password, validation, rate limits)
- [ ] authserver login → session token (argon2id)
- [ ] character create/select endpoints
- [ ] client login + character select/create screens
- [ ] gameserver WS handshake session validation + bans
- [ ] bot scenario: register → login → create char → authed WS session

## Blockers
None. (HUMAN_TODO: VRoid models, Mixamo anims, off-box backup target — none block M1.)

## Next action
M1 task 1: portal registration endpoint. Test = bot registers 20 concurrent
accounts, argon2id hash in DB, wrong-password rejected.

## Ports (ADR-001)
auth=3016 game=3015 admin=3017 portal=3018 control=5003 pg=5004 redis=5005

## Last session log
- 2026-08-11: M0 COMPLETE. Human approved all 8 approvals; executor wrote
  Caddy routes; all 4 subdomains verified over TLS (HTTP 200, cert trusted);
  bot ping/pong over public wss. Tagged m0-complete. M1 begins.
