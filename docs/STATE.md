# STATE — Aetheria living status file

Update at the end of every work block. Read at the start of every session.

## Current milestone
**M3 — Chat & social** (world chat, channels, online presence list; combat core lands with mobs in M3/M4)

## Milestone checklists

### M3 — Chat & social (current)
- [ ] (plan after reading brief §11 + FEEDBACK/HUMAN_TODO)

### M2 — World presence & movement ✅ m2-complete (tag)
- [x] M2 proto messages + Go/GDScript codegen + docs/protocol — DONE:
      Vec3/EnterWorld/EnterWorldAck/MoveIntent/EntityState/WorldSnapshot/
      LeaveWorld; `make content` regeneration.
- [x] world sim + AOI grid — DONE: server/internal/world (20 Hz tick,
      uniform 30 m cells + 3×3 view, position/bounds/speed clamps, snapshot
      deltas, despawn tracking). Unit tests race-clean.
- [x] wire EnterWorld/MoveIntent/LeaveWorld dispatch + EnterWorldAck — DONE:
      connState outbox (single writer), Player.Ready gates snapshots until ack
      enqueued. Round-trip + wrong-account tests green.
- [x] auth spawn load/save — DONE: LoadCharacterSpawn (ownership check,
      COALESCE max_hp) + SaveCharacterPosition; 30 s write-behind + on-leave.
- [x] gameserver zones + control — DONE: havenport 300×300 safe,
      emberfield 600×600; /control/ccu endpoint; class speeds (bd 8.0, sw 7.0).
- [x] bot client world scenarios — DONE: WorldBot, presence, roamer, chaos
      profiles; presence = M2 acceptance (A↔B mutual spawn/move/despawn).
- [x] M2 acceptance live — DONE: presence ALL PASS over local ws AND public
      wss://play.aetheria.../ws TLS; roamer 5 bots ~100 snap/s; chaos fuzz 0
      crashes + fresh conn healthy. bottest now runs full-auth + presence +
      roamer + chaos (all green). Tagged m2-complete.

### M1 — Accounts & Auth ✅ m1-complete (tag)

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
- [x] gameserver WS handshake session validation + bans — DONE:
      gameserver validates Bearer token via shared JWT key + DB ban check
      before ServerHello; invalid/absent token → StatusPolicyViolation close;
      banned (post-login) account → close reason "account_banned"; verified
      live over public wss://play.aetheria... (authed hello, no-token reject,
      bad-token reject, pre-ban-token rejected after ban).
- [x] bot scenario: register → login → create char → authed WS session — DONE:
      `full-auth` profile = M1 acceptance (brief §11). register 201 → login
      token → create char 201 → authed WS hello → no-token reject → bad-token
      reject → wrong-pw 401 → banned-at-handshake (AETHERIA_BANNED_TEST_TOKEN).
      ALL PASS over live public endpoints. bottest now runs full-auth.

## Blockers
None. (HUMAN_TODO: VRoid models, Mixamo anims, off-box backup target — none block M2.)
## Next action
Read brief §11 M3 scope + docs/BRIEF.md chat/combat sections and FEEDBACK.md;
plan M3 sub-tasks. Likely: world chat proto (ChatMessage/channel join), wire
dispatch, hub relay + broadcast, bot chat scenario, client chat HUD.

## Ports (ADR-001)
auth=3016 game=3015 admin=3017 portal=3018 control=5003 pg=5004 redis=5005

## Last session log
- 2026-08-11: M2 complete + tagged m2-complete. Root-caused the chaos-EOF:
  Pong is a raw proto on the wire (M1), bot wrongly expected an Envelope; all
  outgoing server frames now flow through the single-writer outbox (fixed a
  real concurrent-write bug), Player.Ready gates snapshots until EnterWorldAck.
  botclient presence/roamer/chaos ALL PASS local + presence over public wss
  TLS; bottest wired to all 4 profiles (green); M2 tagged + pushed.
- 2026-08-11: M2 core + acceptance DONE. World sim (20 Hz, AOI 30 m cells,
  3×3 view, speed/bounds clamps), wire EnterWorld/MoveIntent/LeaveWorld with
  single-writer outbox (fixed concurrent-write bug: hello/pong/ack now flow
  through the writer goroutine), Player.Ready gates snapshots until
  EnterWorldAck is enqueued, auth LoadCharacterSpawn/SaveCharacterPosition,
  zones (havenport 300² safe, emberfield 600²), /control/ccu. botclient:
  WorldBot + presence (M2 acceptance: A↔B mutual spawn/move/despawn), roamer
  (5 bots, ~100 snap/s), chaos (211 fuzz frames, 0 crashes, fresh conn
  healthy) ALL PASS live (local ws). Root-caused EOF: Pong is a raw proto
  (M1 wire), bot wrongly expected Envelope-wrapped; PingRoundTrip now accepts
  both. Roamer/chaos unique-name fixes. CI green, make test + race green.
- 2026-08-11: M1-5 + M1-6 DONE. Gameserver WS handshake validates Bearer
  session (shared JWT key + DB ban re-check before ServerHello); no/bad token
  closed StatusPolicyViolation, banned account closed "account_banned". New
  bot `full-auth` profile = M1 acceptance (register→login→create char→authed
  WS hello + all rejection paths) ALL PASS over public wss/api TLS. bottest
  target now runs full-auth. M1 acceptance criteria met; tag m1-complete.
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
