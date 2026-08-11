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
- [x] portal registration (email+password, validation, rate limits) — DONE:
      authserver /auth/register + /auth/login; portal /register form + proxy;
      argon2id hashing (OWASP params), email/password validation, Redis
      per-IP rate limit (register 5/15m, login 10/15m), 2-slot argon2
      semaphore (hash-storm + OOM guard). bot register: 20/20 concurrent
      green; argon2id verified in DB; public portal works over TLS.
- [x] authserver login → session token — DONE: HS256 JWT (golang-jwt),
      ≤24 h TTL (AETHERIA_SESSION_KEY/TTL_HOURS), login records `logins`
      audit row. bot login profile: token issued + expires_at, wrong
      password 401, unknown email 401, banned 403 — all verified live.
- [x] character create/select endpoints — DONE: /auth/characters/create
      (name rules 2-16 alnum/_, 2 classes blade_dancer|spellweaver, roster
      cap 6, name-taken 409, soft-delete reaping) + /auth/characters (list);
      Bearer-token auth. bot create-char profile: 401/400/201/409 + roster
      all green.
- [x] client login + character select/create screens — DONE: Login.tscn,
      CharSelect.tscn (list/create/play-stub), Boot hands off; session logic
      decoupled (Session/ClientConfig/ApiClient) and headless-tested;
      config.json next to exe (prod defaults); live Godot test
      (register→login→roster via ApiClient) passes; exported client boots.
- [ ] gameserver WS handshake session validation + bans
- [ ] bot scenario: register → login → create char → authed WS session

## Blockers
None. (HUMAN_TODO: VRoid models, Mixamo anims, off-box backup target — none block M1.)

## Next action
M1-5: gameserver WS handshake session validation. Client sends Bearer token
in the WS handshake; gameserver validates it (shared session key) + checks
ban status before ServerHello. Test = authed client connects + gets
ServerHello; bad/absent token rejected; banned account rejected.

## Ports (ADR-001)
auth=3016 game=3015 admin=3017 portal=3018 control=5003 pg=5004 redis=5005

## Last session log
- 2026-08-11: M1-4 DONE. Godot Login + CharSelect scenes, session/ApiClient
  layer headless-tested, config.json next to exe; live Godot flow
  register→login→roster green; exported client boots to Login.
- 2026-08-11: M1-3 DONE. Character create/list endpoints live: name rules,
  class whitelist, roster cap, duplicate 409, Bearer auth; bot profile
  verifies 401/400/201/409 + roster.
- 2026-08-11: M1-2 DONE. Login issues HS256 JWT (24 h, key+ttl from env);
  logins audited; bot profile verifies token + 401/403 rejection paths.
- 2026-08-11: M1-1 DONE. Portal registration live over TLS: authserver
  /auth/register (argon2id, validation, Redis rate-limit, 2-slot hash
  semaphore after OOM fix), portal /register form + proxy, bot register
  20/20, argon2id in DB, duplicate/weak-password rejected.
- 2026-08-11: M0 COMPLETE. Human approved all 8 approvals; executor wrote
  Caddy routes; all 4 subdomains verified over TLS (HTTP 200, cert trusted);
  bot ping/pong over public wss. Tagged m0-complete. M1 begins.
