# STATE — Aetheria living status file

Update at the end of every work block. Read at the start of every session.

## Current milestone
**M5.5 — Client UI/UX Overhaul** (mandatory; blocks M6 AND blocks the
`m5-complete` tag — the first Windows-build playtest verdict was "no-effort UI,
didn't feel like a game"). M5 server acceptance is met live; m5-complete is held
until M5.5 §5 passes and the human signs the gallery off in FEEDBACK.md.
See docs/M5_5_UI_UX_OVERHAUL.md + docs/AGENCY_INTEGRATION.md.

## Milestone checklists

### M5 — Quests & Town (current)
- [x] quest framework + 15-quest Havenport chain + NPC givers + persistence
- [x] quest UI client layer: tracker, log, NPC dialog (accept/turn-in)
- [x] `make questrun` master regression — ALL PASS local (run14, level 10) and
      against live production (15/15 quests, 30 kills, 7 pickups).
- [x] production deploy of M4/M5 server code (`make deploy`, all 4 healthy).
- [x] `make bottest` green against production (M1–M4 acceptance).
- [x] full playable world client: session/transport, themed HUD, procedural
      Emberfield, movement/combat/chat/quests (see Last session log).
- [x] server tuning knobs (AETHERIA_TUNE_SPEED / _RESPAWN_MS) — fast questrun
      + snappier playtests; live at 4x speed / 5 s respawn.
- [x] client download page (portal /download) + make publish-client + CI
      questrun job (pushes to main only).
- [ ] inventory/equip/vendor UI in Godot (needs M4 InventoryList/equip wire
      push to render) — M6+ first client pass.
- [ ] tag m5-complete — BLOCKED on M5.5 §5 (human gallery sign-off in
      FEEDBACK.md). Do not tag until M5.5-complete is tagged.

### M5.5 — Client UI/UX Overhaul (current; blocks M6 + m5-complete)
Rollout order per docs/AGENCY_INTEGRATION.md §5:
- [x] 1. Commit M5_5 + AGENCY_INTEGRATION docs; update AGENTS.md standing rules
      (screenshot gate, session-per-task, §2 block contract) + docs/BLOCK_PROMPT.md.
- [x] 2. Build M5.5 §1 screenshot pipeline — DONE this block: user-space Xvfb +
      Mesa llvmpipe prefix (no sudo) + LD_PRELOAD xkbcomp shim, ScreenshotTour.gd
      (16 offline stages covering login×3, charselect×3, HUD×10; 2 server-gated
      stages skip offline), `make screenshots` target, adminserver `/screens`
      gallery (bearer-token gate, bind-mounted volume, side-by-side compare),
      docs/runbook.md. Verified: `make screenshots` → 16 PNGs + webp thumbs →
      published to https://admin.aetheria.apps.deployden.tech/screens?t=<token>
      (404 without token). `make test` green (go test + godot headless 4/4).
      screens: ac9c4cb awaiting review.
- [ ] 3. Implement AGENCY_INTEGRATION §1.1 enqueuer + §1.2 worker handler +
      §1.3 `.manual` expiry (agency-os repo PR), register/repair job 12, add
      GLM-5.2 to MODEL_PRICING. Also add `aetheria_screens` + `aetheria_gate` +
      `aetheria_human_todo` to the agency-os `approval_type` enum (migration) —
      they don't exist yet, so this block staged the first screens review via
      the existing `deploy` type (approval id=60) as a stopgap.
- [ ] 4. Stage first `aetheria_screens` approval from the pipeline's maiden run
      (validates §3 end-to-end) — staged this block via orch.
- [ ] 5. Dashboard §4 changes as a dev-task PR on agency-dashboard.
- [ ] 6. Remove `.manual`, enable job 12, watch two unattended blocks go green.
- [ ] M5.5 §2 art direction: design tokens + theme resource, ornate-panel
      layout skeleton, no-default-theme automated check.
- [ ] M5.5 §3 game feel ("juice"): FCT, hit feedback, target rings, cast bars,
      skill-bar cooldown sweep, camera spring, audio, level-up/quest banners.
- [ ] M5.5 §4 onboarding & discoverability: hint toasts, first-giver beacon,
      objective chevrons/minimap markers, H help + ESC menu, empty states.
- [ ] M5.5 §5 acceptance: make screenshots deterministic + published; zero
      default-theme controls; §2/§3/§4 implemented; VRoid chars animated
      (blocked on HUMAN models); 60fps@1080p medium; human verdict
      "looks and feels like a real game" → tag m5.5-complete → then m5-complete.
- [ ] HUMAN follow-up: screenshot_bot account + GM token for the 2 server-gated
      tour stages (logged in HUMAN_TODO.md).

## Milestone checklists

### M4 — Economy (current)
- [x] M4 core — items, grid inventory (24), equipment (weapon/chest/accessory)
      with stat application, ground drops (TypeDrop, 3.0 m pickup radius,
      2-min TTL, single-claim), per-mob gold reward + loot rolls on kill,
      vendors (buy/sell at vendor_price), audited gold ledger on every
      mutation (mob_kill/pickup_gold/vendor_buy/vendor_sell/gm_grant) —
      DONE: server/internal/world/economy.go + combat_sim.go killMob hooks;
      world-layer unit tests all pass (22 s).
- [x] M4 seeds + contentlint — DONE: shared/content/{items,drops,npcs}/ + mob
      gold_reward; contentlint validates required keys (items/drops/npcs).
- [x] M4 store persistence — DONE: ApplyGoldLedger (single tx, rejects
      insufficient-gold rows), LoadCharacterItems/SaveCharacterItems,
      CharacterSpawn gold; auth integration tests gated on AETHERIA_PG_DSN.
- [x] M4 wire messages + codegen — DONE: PickupItem/EquipItem/UnequipItem/
      SellItem/BuyItem/LootEvent; make content regenerated Go + GDScript.
- [x] M4 wire dispatch + events — DONE: hub dispatch cases; LootEvent now
      carries ok/balance and rejections emit ok=false + error; pickup reports
      the item's instance id.
- [x] M4 bot trader scenario — DONE: kill boar → loot ground drop → sell;
      botclient -profile trader; wired into make bottest.
- [x] M4 acceptance live — DONE: make bottest green (full-auth/presence/
      roamer/chaos/chat/combat/trader); make test green incl. contentlint +
      Godot headless; sum(gold_ledger)=415=sum(characters.gold) live.
      Ready to tag m4-complete.

### M3 — Chat & social (current) ✅ m3-complete (tag)
- [x] chat relay + mute — DONE: server `say` (30 m) / `world` (zone-wide)
      channels + MuteCharacter; bot chat scenario (A↔B world relay, say
      non-leak at 200 m apart, unmuted resend) ALL PASS live; bottest runs
      chat. Unit TestChatWorldAndMute.
- [x] 50-bot combat soak ALL PASS (30m formal acceptance) — DONE:
      N=50, cycles=765, hardFails=0, softTimeouts=736, negHP=0, tick p99 max
      20.08ms (< 50ms ceiling). Root-caused the last intermittent client
      disconnect: the per-cycle 120s context expiring mid snapshot-body read
      makes coder/websocket close the socket to unblock the read, surfacing
      net.ErrClosed on the body path (no finishRead ctx.Err override there);
      a budget-end self-teardown, not a server fault. Soak now classifies it
      as a soft timeout. Server proved clean: 0 socket-write failures, only
      EOF teardowns (now logged with char_id).
- [x] M3 combat-core acceptance verified live — DONE: tagged m3-complete.
      Combat bot scenario (kill boar +12 XP → pull Ashmaw → die → respawn
      HP=100) ALL PASS over local ws and public wss; make bottest full suite
      green (full-auth + presence + roamer + chaos + combat). Remaining M3
      combat sub-tasks (loot/gold/level-up) and M3 chat/social list still
      open — flagged in Next action.
- [x] combat core + mobs + bot combat scenario — DONE: mob defs (10), bands
      1–3 (town → deep field), skills (15 incl. blade_strike/mob_bite/gore),
      aggro/leash, XP + death + respawn at shrine; live bot scenario kills a
      boar (+12 XP) → pulls Ashmaw → dies → respawns HP=100. Unit tests + bot
      ALL PASS. `make bottest` now runs combat profile. (Planned M3 chat/social
      list still TBD — combat landed first per brief §11.)
- [x] zone bounds position-derived — DONE: havenport = 50×50 safe pocket
      inside emberfield 600×600; walking out transitions zone
      (TestWalkingOutOfTownTransitionsZone). Deterministic spawn placement
      (sorted-def order).

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
m5-complete tag is BLOCKED on M5.5 §5 (human gallery sign-off in FEEDBACK.md).
VRoid models + UI art are HUMAN_TODO (M5_5 §2) — they gate the "VRoid chars
animated" acceptance line but NOT the rest of M5.5; ship everything else with
flat-color token placeholders.

## Next action
M5.5 rollout item 3: implement the Agency OS enqueuer + worker handler
(docs/AGENCY_INTEGRATION.md §1) as a small PR on the agency-os repo, repair job
12, add GLM-5.2 to MODEL_PRICING. Meanwhile, the human should review
screens ac9c4cb at the gallery URL and write verdicts in FEEDBACK.md.

## Ports (ADR-001)
auth=3016 game=3015 admin=3017 portal=3018 control=5003 pg=5004 redis=5005

## Last session log
- 2026-08-15 (M5.5 kickoff): human playtest verdict on first Windows build was
  "no-effort UI, didn't feel like a game" → created docs/M5_5_UI_UX_OVERHAUL.md
  (mandatory UI/UX overhaul, blocks M6 + m5-complete) and
  docs/AGENCY_INTEGRATION.md (Agency OS loop automation: enqueuer job +
  worker task type + dashboard visibility). Updated AGENTS.md standing rules
  (screenshot-gated client changes, session-per-task work blocks, block
  contract) + created docs/BLOCK_PROMPT.md. Built M5.5 §1 screenshot pipeline:
  the VPS has no GPU/display/sudo, so Xvfb+Mesa were extracted into a
  user-space prefix (~/.local/xvfb-prefix) with an LD_PRELOAD shim that
  redirects the X server's hardcoded /usr/bin/xkbcomp into the prefix;
  llvmpipe software GL confirmed ("OpenGL 4.5 Mesa llvmpipe"). client/tools/
  ScreenshotTour.gd drives the real scenes/HUD via public APIs across 16
  offline stages (login×3, charselect×3, HUD×10) + 2 server-gated stages that
  skip offline. make screenshots → docs/screens/<sha>/ + webp thumbs, published
  to /home/agency/aetheria/screens (bind-mounted into aetheria-adminserver).
  server/internal/screens gallery on adminserver /screens (bearer-token gate,
  AETHERIA_SCREENS_TOKEN, side-by-side compare), docker-compose volume added,
  admin container rebuilt. Verified end-to-end over TLS
  (admin.aetheria.../screens → 200 w/ token, 404 w/o). docs/runbook.md
  created with the pipeline section. make test green (go test all ok incl. new
  screens package; godot headless 4/4 PASS). Staged aetheria_screens approval
  for sha ac9c4cb. HUMAN_TODO: VRoid models + UI art (M5.5 §2) + screenshot_bot
  account for the 2 server-gated tour stages. Next: rollout item 3 (agency-os
  enqueuer+worker PR).
- 2026-08-14: M5 ACCEPTED LIVE + playable Godot client shipped. Quests: fixed
  the magma-stall root cause (respawn-guard race — death combat event precedes
  the hp=0 self snapshot by a tick; bot died, every auto-attack hit ErrDead) →
  quester ALL PASS local (run14) and against production. Wire/codegen fixes:
  GDScript floats now fixed32 (was LEN, mismatching Go), repeated-field decode
  + `self`→`_self`, MP/XP on EntityState (13–16) + Player.SelfState. Client:
  WorldSession transport, AetheriaTheme (Cinzel/Exo2, night-ember), full HUD
  (frames/bars/skill bar/chat/tracker/log/dialog/death/minimap), WorldEntity
  placeholders, procedural Emberfield scene + terrain shader, WASD/orbit-cam/
  targeting, CharSelect→World. Tests: test_world_session + World boot in
  test_scenes; make test green. Ops: deployed M4/M5 to production (all 4
  healthy), bottest green live, server tuning (4x speed, 5 s respawn),
  portal /download + make publish-client + Windows/Linux zips live, CI questrun
  job on pushes. Commits 4804bbb..ae5c6e9.
- 2026-08-12/13: M4 COMPLETE — economy core + acceptance live. make test +
  bottest (full-auth/presence/roamer/chaos/chat/combat/trader) all green;
  sum(gold_ledger)=415=sum(characters.gold) verified against live DB;
  ready to tag m4-complete. This block: fixed LootEvent wire contract
  (ok/balance populated, rejections emit ok=false+error, pickup returns the
  item's instance id); added botclient trader scenario (kill boar → loot
  ground drop → sell, gold 3→6) and fixed its flakiness — it targeted only
  the single forest_boar (deterministic band-1 anchor), so it now orbits all
  10 band-1 anchors and boar_hide drop chance is 1.0 (matches test content;
  boar hides are a guaranteed basic drop); fixed pre-existing flaky JWT test
  (flipping the last base64url sig char hits padding bits — flip a mid-sig
  char instead); made ledger DB integration tests collision-proof (random
  email/char-name suffix).
- 2026-08-12: M3 COMPLETE — chat relay/mute + combat core + soak all green;
  make test + bottest (full-auth/presence/roamer/chaos/chat/combat) pass;
  moved m3-complete tag to HEAD. Chat relay: server say (30 m)/world
  (zone-wide) + MuteCharacter, bot chat scenario (world relay, say non-leak,
  unmuted resend) ALL PASS live; unit TestChatWorldAndMute. M4 begins.
- 2026-08-12: 50-bot combat soak ALL PASS — formal 30m acceptance met
  (cycles=765 hardFails=0 softTimeouts=736 negHP=0, p99 max 20.08ms).
  Root-caused the last 2 intermittent hard disconnects (DISC char=… el=119s
  err=failed to read: use of closed network connection): the client's
  per-cycle ctx (cap 120s) expiring while blocked mid snapshot-body read —
  coder/websocket setupReadTimeout's AfterFunc calls c.close() to unblock
  (conn.go:193-196); the msgReader body path has no finishRead so net.ErrClosed
  survives instead of ctx.Err() (244 sibling cycles showed the ctx path).
  Server logs proved clean throughout (only EOF teardowns, 0 socket-write
  fails). Fix: Combat returns ctx.Err() when the budget expired → soak buckets
  soft. Added server conn-teardown logging (char_id, socket-write-fail warn),
  -soak-verbose flag (per-frame combat debug), and a seed-register retry
  (contended-box 10s register timeout). One transient p99 spike (56ms) in an
  earlier run traced to box contention (load 6.03, ClickHouse pegging a core),
  not the gameserver (p50 stayed ~3.5ms). Committed 11728bb + 120ecb6.
- 2026-08-12: M3 combat core live-verified + world-bounds clamp. Concurrent
  soak (local + public wss, up to 6 bot aggressors): intermittent EOF +
  runaway bots walking to (801,782) off the world uncovered a real server bug
  — applyMove only clamped when the target position was inside a zone, so a
  straight walking bot could leave all zones and escape unbounded. Fixed:
  always clamp to outermost bounds (TestWorldClamp); leftover failures after
  the fix are pure resource contention (4 band-1 mobs / 30 s respawn for 6
  hunters → scenario timeout, not server drops). Hunting with no hostile now
  orbits the nearest spawn anchor instead of drifting; concurrent runCombat
  names/emails get random suffix (no more same-second 409). Root-caused
  combat-EOF earlier in block: server idles socket after 10 s no-inbound
  frames, sim emits snapshots only on change → idle bot blocks in
  ReadSnapshot; fixed with concurrent StartHeartbeat (4 s ping). Ashmaw stop
  threshold lowered 14→8 m to sit inside its 12 m aggro radius. make bottest
  runs combat profile (full suite green 4/4). Committed; m3-complete tag next.
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
