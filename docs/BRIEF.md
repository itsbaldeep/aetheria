# PROJECT AETHERIA — Open-World 3D Anime MMORPG
## Master Project Brief, MVP Roadmap & Autonomous Dev Workflow
### (This document is the kickoff prompt for the OpenCode agent. Read it fully before writing any code. It is the single source of truth until superseded by files in `/docs`.)

---

## 1. Vision

A free-to-play, open-world 3D MMORPG for PC in the spirit of Perfect World, Last Chaos, and classic WoW — anime/toon visual style, hybrid tab-target + aimed-skill combat, persistent world, parties, trading, an auction house, GM tooling, and a public web portal with voting and donations.

**Working title:** Aetheria (rename is a config value, not a code change — keep all branding in one place: `shared/branding.json`).

**MVP definition:** a stranger can create an account on the portal, download the client, log in, make a character (1 race, 2 classes), quest and fight through a town + open field + small dungeon (levels 1–10), party with friends, trade, use the auction house, chat — while a GM administers the live server from in-game commands and a web dashboard, on a single 8 GB VPS supporting up to 200 concurrent players.

---

## 2. Locked Technical Decisions (do not revisit without human approval)

| Area | Decision |
|---|---|
| Client engine | **Godot 4.x (latest stable), GDScript**, native PC export (Linux + Windows) |
| Game/auth/admin servers | **Go (latest stable)**, single repo, one binary per service |
| Web portal | **Go backend (same repo) + server-rendered templates + HTMX**; no SPA framework |
| Database | **PostgreSQL 16** (persistent state) + **Redis** (sessions, pub/sub, ephemeral state) |
| Client↔server transport | **WebSocket over TLS** (Godot `WebSocketPeer` ⇄ Go `nhooyr.io/websocket` or `gorilla/websocket`) |
| Wire format | **Protocol Buffers (proto3)**. One `.proto` source of truth in `shared/proto/`; codegen for Go; for Godot use godobuf or a generated GDScript packer — agent picks the most maintainable and documents it in `/docs/adr/` |
| Combat | **Hybrid**: tab-target auto-attack + cooldown skills, plus a minority of aimed/ground-targeted (AoE template) skills |
| Art style | **Anime/toon**: cel/toon shading in Godot, VRoid-based characters, stylized low-poly environments (see §9 Asset Pipeline) |
| Authority model | **Server-authoritative everything.** The client renders and requests; it never decides. |
| Tick rate | Server simulation **20 Hz**; client interpolation ~100 ms buffer; client prediction for self-movement only |
| Scale target | 200 CCU on one 8 GB / 4 vCPU VPS, everything colocated (all services + Postgres + Redis + OpenCode agent) |
| Repo layout | **Monorepo** (see §4) |

**Why these matter (context for the agent):** WebSocket+TLS avoids NAT/firewall pain for players and is fully supported by Godot; protobuf gives a typed contract across Go and GDScript; 20 Hz with interpolation is genre-appropriate for hybrid combat at this scale; a monorepo keeps the shared protocol honest.

---

## 3. System Architecture

All processes run on the single VPS under systemd, fronted by Caddy (automatic TLS).

```
                    ┌──────────────────────── VPS (8 GB / 4 vCPU) ────────────────────────┐
                    │                                                                      │
 Player laptop      │   Caddy (:443, TLS)                                                  │
 ┌────────────┐     │   ├── wss://play.<domain>/ws   ──► gameserver   (:7001, WebSocket)   │
 │ Godot      │◄────┼──►├── https://<domain>          ──► portal      (:7003, HTTP)        │
 │ client     │     │   ├── https://admin.<domain>    ──► adminserver (:7002, HTTP)        │
 └────────────┘     │   └── https://api.<domain>/auth ──► authserver  (:7000, HTTP)        │
                    │                                                                      │
                    │   gameserver ◄── Redis pub/sub ──► adminserver / portal              │
                    │   all services ──► PostgreSQL 16 (localhost:5432)                    │
                    │   all services ──► Redis (localhost:6379)                            │
                    │                                                                      │
                    │   opencode agent (tmux session) — builds, tests, deploys             │
                    └──────────────────────────────────────────────────────────────────────┘
```

### Services

**authserver (Go, HTTP :7000)** — account registration/login (argon2id password hashing), issues short-lived signed session tokens (JWT or PASETO) that the game server validates; email is username; rate-limited; also serves character-list and character-create endpoints.

**gameserver (Go, WebSocket :7001)** — the authoritative world. Fixed 20 Hz tick loop. Owns: movement validation, spatial interest management (grid-based AOI, ~30 m cells, broadcast only to nearby players), combat resolution, mob AI (state machines: idle/patrol/aggro/leash), loot rolls, quest progress, XP/leveling, party logic, trade sessions, auction transactions, chat routing, GM command execution. Zones are goroutine-owned shards within the one process (town, field, dungeon instances). Persistence via write-behind to Postgres (dirty-flag flush every 30 s + on logout + on server drain). Exposes a private HTTP control endpoint (:7011, localhost-only) for the admin server: broadcast, kick, drain, reload content tables.

**adminserver (Go, HTTP :7002)** — web dashboard (server-rendered + HTMX): live player list (kick/ban/mute/teleport/grant), server health (tick duration, CCU, goroutines, memory), log tail, content browser, economy counters (gold created vs destroyed per hour), restart/drain buttons. Separate admin accounts with TOTP 2FA. Every admin action writes an `audit_log` row.

**portal (Go, HTTP :7003)** — public site: account registration/management, client download page (serves the latest client builds), server status widget (CCU, uptime), rankings (level/wealth top lists, refreshed by cron), news posts (managed from adminserver), **voting** (outbound links to topsite aggregators + signed postback/callback endpoint that credits vote points to the account), **donations** (provider-agnostic `PaymentProvider` interface; ship with a sandbox/mock provider and a manual-approval flow; real Stripe/PayPal keys are human-supplied config later — never hardcode, never invent credentials). Vote points and donation credits are spendable in an in-game cosmetic/QoL shop table (no pay-to-win items in MVP).

### Data flow rules
- The client sends **intents** (`MoveIntent`, `CastSkill{skill_id, target}`, `TradeOffer`…), never state.
- The game server sends **authoritative snapshots + events** (entity deltas for AOI, combat results, chat, system messages).
- Anything valuable (items, gold, XP, trades, auction) is transacted in Postgres with row-level locking or serializable transactions — dupes are the classic MMO killer; every economy mutation goes through one audited code path.

---

## 4. Repository Layout (monorepo)

```
aetheria/
├── AGENTS.md                  # agent operating manual (see §11 — write this file first)
├── docs/
│   ├── adr/                   # architecture decision records (one md per decision)
│   ├── protocol.md            # generated protocol reference
│   └── runbook.md             # ops: deploy, backup, restore, incident steps
├── shared/
│   ├── proto/                 # .proto files — single source of truth
│   ├── branding.json
│   └── content/               # game data as versioned JSON/CSV seeds (items, mobs, skills, quests, drop tables, NPCs, zones)
├── server/
│   ├── cmd/{authserver,gameserver,adminserver,portal}/
│   ├── internal/...           # domain packages: world, combat, inventory, party, trade, auction, chat, gm, persistence, net
│   └── go.mod
├── client/                    # Godot 4 project
│   ├── project.godot
│   ├── scenes/  scripts/  assets/  shaders/
├── tools/
│   ├── botclient/             # headless Go bot that speaks the real protocol (see §12)
│   ├── contentlint/           # validates shared/content against schemas
│   └── loadtest/              # spawns N botclients
├── deploy/
│   ├── systemd/  caddy/  migrations/  backup.sh  deploy.sh
└── Makefile                   # make build|test|migrate|deploy|export-client|loadtest
```

---

## 5. Database Schema (initial migration set — extend via numbered migrations only)

Core tables (columns indicative, agent finalizes in migration files):

- `accounts` (id, email, pass_hash, created_at, banned_until, ban_reason, vote_points, donation_credits, is_gm, totp_secret nullable)
- `characters` (id, account_id, name unique, class, level, xp, gold, zone_id, pos_x/y/z, hp, mp, stats jsonb, playtime_seconds, deleted_at)
- `items_instances` (id, owner_char_id nullable, container enum[inventory,equipment,bank,trade_escrow,auction_escrow], slot, item_def_id, quantity, bound bool, rolled_stats jsonb)
- `item_defs`, `mob_defs`, `skill_defs`, `quest_defs`, `npc_defs`, `drop_tables`, `zone_defs` — loaded from `shared/content` seeds; editable later via admin content editor (post-MVP)
- `character_quests` (char_id, quest_id, state, progress jsonb)
- `character_skills` (char_id, skill_id, rank)
- `friends` (char_id, friend_char_id, status)
- `auction_listings` (id, seller_char_id, item_instance_id, buyout_price, created_at, expires_at, state)
- `trade_log`, `gold_ledger` (every gold delta with reason — the economy audit trail), `audit_log` (admin/GM actions), `logins`, `votes` (account_id, site, ip, credited_at), `donations` (account_id, provider, amount, status, external_ref)
- `client_builds` (version, platform, file_path, sha256, published_at)

Rules: all money/item mutations inside DB transactions; `gold_ledger` is append-only; soft-delete characters; never store plaintext secrets.

---

## 6. Gameplay Spec (MVP content)

**Race/classes:** one race ("Human" placeholder). Two classes:
- **Blade Dancer** (melee): auto-attack, Cleave (frontal cone, aimed), Rush (gap closer), Whirlwind (PBAoE), Battle Cry (self-buff), Execute (tab-target finisher).
- **Spellweaver** (ranged): auto-attack (bolt), Fireball (tab-target), Frost Nova (PBAoE root), Meteor (ground-targeted AoE template, aimed), Mana Shield (self), Blink (short teleport).

Skill ranks 1–3 unlock by level. Stats: HP/MP, STR/INT/DEX/VIT, basic derived formulas (document them in `/docs/combat.md`; keep every formula in one Go package `internal/combat/formulas.go`).

**Zones:**
1. **Havenport (town)** — safe zone, no combat. NPCs: class trainer, general vendor, weapon/armor vendor, auctioneer, banker, quest givers, mailbox (post-MVP), respawn shrine.
2. **Emberfield (open field)** — ~600×600 m playable, level 1–10 mobs in banded areas (boars → wolves → bandits → corrupted treants → field mini-boss "Ashmaw"), resource-free MVP (no gathering), 2 dynamic respawn shrines.
3. **Hollow Depths (dungeon)** — instanced per party, 3 rooms + boss "Warden Kryx" (one aimed AoE mechanic to dodge), lockout 1 h, level ~8–10.

**Mobs:** ~10 types, shared AI framework (aggro radius, leash, threat table, 1–2 skills for elites/bosses).

**Quests:** ~15 (kill N, collect drops, talk-to, one dungeon quest chain), linear breadcrumb from town through field bands into the dungeon. Quest data lives in `shared/content/quests/*.json`.

**Systems in MVP:** inventory (grid, stackable), equipment (weapon/head/chest/legs/feet/2 accessories), loot (per-player loot rolls in party), vendors (buy/sell), XP/leveling 1–10 (curve in content data), death → respawn at shrine with 10% durability-free gold-free penalty (just walk back; keep it friendly), global/zone/party/whisper chat with mute support, **party** (max 5, shared XP within level range, round-robin or per-player loot), **friends list** (online status), **player-to-player trade** (two-phase confirm, escrow, logged), **auction house** (list/buyout only — no bidding in MVP; 5% gold sink fee).

**Explicitly OUT of MVP** (do not build, do not stub UIs): guilds, mounts, pets, crafting, gathering, PvP, mail, bank expansion, achievements, weather, day/night, character customization beyond preset faces/hair colors, more races/classes, localization.

---

## 7. Client Spec (Godot 4)

- Scenes: login → character select/create → world. World scene streams the current zone only.
- Third-person orbit camera (mouse), WASD + click-to-move optional, space jump (cosmetic, server clamps), tab-targeting + click targeting, skill bar (1–9), aimed skills show a ground/cone telegraph before confirm.
- Client-side prediction for own movement; server reconciliation (snap if divergence > threshold); other entities interpolated ~100 ms behind.
- UI: HUD (self/target frames, cast bars, XP bar, skill bar, minimap-lite), inventory/equipment, quest log + tracker, party frames, chat tabs, trade window, auction browser, vendor window, settings (keybinds, volume, render scale, fullscreen), GM console (hidden unless account `is_gm`).
- Toon rendering: single shared cel-shader material family + outline pass; one post-processing environment per zone. Keep it cheap — target 60 fps on integrated GPUs at medium.
- Client reads server address from `client/config.json` next to the executable (default = production domain) so the human can point it at localhost or the VPS freely.
- **Headless-testability requirement:** all protocol/session logic lives in scripts decoupled from rendering nodes, so `godot --headless` can run integration scripts in CI.

---

## 8. Admin & GM Spec

**In-game GM commands** (account flag `is_gm`, every use → `audit_log`): `/announce`, `/kick`, `/ban <dur>`, `/mute <dur>`, `/tp <player|x,y,z|zone>`, `/summon <player>`, `/item <def_id> [qty]`, `/gold <amt>`, `/level <n>`, `/spawn <mob_def> [n]`, `/killmob`, `/invisible`, `/speed <mult>`, `/heal`, `/serverinfo`.

**Web dashboard:** live CCU + player table (search, click → detail: inventory, gold, position, actions), ban/mute management, chat monitor + mute, server health panel (tick p50/p99, memory, goroutines, Redis/PG status), economy panel (gold ledger charts, top wealth), news post editor (publishes to portal), client build publisher (upload/promote a build → appears on portal download page), drain + restart controls, audit log viewer.

---

## 9. Asset Pipeline (anime style — the one part that is NOT fully autonomous)

Ground truth: the agent cannot author 3D art. Strategy:

1. **Characters:** VRoid Studio base models (human supplies 2–4 exported `.vrm` files — male/female per class silhouette). Agent integrates via a VRM addon for Godot 4, retargets **Mixamo** animations (idle, run, jump, 2–3 attacks, cast, hit, death) onto them, and applies the project toon shader.
2. **Environment/props/mobs:** CC0/CC-BY stylized low-poly packs (Kenney, Quaternius, KayKit, Poly Pizza) — toon-shaded low-poly reads as "anime-adjacent" and coherent. Agent may download only CC0/CC-BY assets, records every asset in `client/assets/CREDITS.md` (source, license, URL), and never ships anything with unclear licensing. **Never extract assets from Perfect World, Last Chaos, WoW or any commercial game.**
3. **Placeholder-first rule:** every feature ships first with capsule/box placeholders; asset polish is a separate milestone. Gameplay never waits for art.
4. **HUMAN TASK list:** when the agent needs an asset it cannot legally obtain, it adds a line to `HUMAN_TODO.md` (what, why, suggested source) and continues with placeholders.

---

## 10. Security & Anti-Cheat Guardrails (non-negotiable)

- Server validates every intent: movement speed/teleport checks, skill range/cooldown/cost/LoS, inventory ownership on every item op, trade/auction escrow, level-gated actions.
- Rate-limit per connection (packets/sec, chat/sec, login attempts); disconnect + log abusers.
- TLS everywhere; argon2id; tokens expire ≤ 24 h; admin dashboard requires TOTP; admin/control ports bound to localhost (only Caddy exposed).
- Secrets in `/etc/aetheria/env` (systemd `EnvironmentFile`), never in the repo. `.env.example` documents required keys.
- SQL only via parameterized queries; portal forms CSRF-protected; donations/vote callbacks signature-verified.
- The agent must never weaken these to make a test pass.

---

## 11. MVP Roadmap — Milestones & Acceptance Criteria

Work strictly in order. A milestone is DONE only when every acceptance criterion passes via automated test or scripted verification, `make test` is green, and a tagged release exists. Do not start milestone N+1 with N incomplete. Estimated effort assumes an agent working continuously; treat estimates as ordering weights, not deadlines.

### M0 — Foundations (repo, CI, deploy skeleton)
Build: monorepo scaffold per §4; AGENTS.md; Makefile; Postgres+Redis provisioned; migration tool wired (goose or golang-migrate); proto toolchain + codegen for Go and Godot; Caddy + systemd units; `deploy.sh` (build → migrate → restart services); empty services that boot, log, and serve health endpoints; Godot project boots to an empty scene; CI = git hook or script that runs `make test` + `gofmt` + `go vet` + contentlint.
**Accept:** `make deploy` from a clean checkout brings up all four services green under systemd; `curl` health checks pass through Caddy over TLS; a client export (`make export-client`) produces runnable Linux+Windows binaries; bot connects to gameserver WS and completes a protobuf ping/pong.

### M1 — Accounts & Auth
Build: portal registration (email+password, validation, rate limits), authserver login → token, character create/select endpoints (name rules, class choice), client login + character select/create screens, session validation on the gameserver WS handshake, bans enforced at login.
**Accept:** bot script registers → logs in → creates character → establishes an authenticated WS session; wrong password / banned account rejected; 20 concurrent registrations succeed; passwords verified argon2id in DB.

### M2 — World Presence & Movement (the "two clients see each other" milestone)
Build: zone loading (Emberfield graybox terrain), spawn/despawn, MoveIntent → server validation → 20 Hz snapshots, AOI grid, client prediction + interpolation, other-player rendering with name plates, disconnect handling, position persistence.
**Accept:** 2 bot clients see each other's movement with correct AOI (entering/leaving range); speed-hacked bot intent is clamped and logged; 100 bots roam for 10 min with tick p99 < 50 ms and zero goroutine leaks; human check: run two clients on laptop against VPS, movement feels smooth (record in FEEDBACK.md).

### M3 — Chat, Combat Core & Mobs
Build: chat channels + mute; target selection; auto-attack; the 12 class skills incl. aimed telegraphs; damage/mitigation formulas; mob framework (spawner, AI states, threat, leash); 10 mob types in banded areas; XP/level 1–10; death/respawn; combat log messages.
**Accept:** bot kills a boar and gains XP; bot dies to Ashmaw and respawns at shrine; cooldown/range/cost violations rejected server-side (unit tests per skill); ground-AoE hits only entities in template; 50-bot combat soak (30 min) with no crash, no negative HP/XP, tick p99 < 50 ms.

### M4 — Items, Inventory, Loot, Vendors
Build: item defs + instances, grid inventory, equipment slots with stat application, per-player loot rolls, gold, vendors (buy/sell), item pickup radius rules, `gold_ledger` on every mutation.
**Accept:** transactional tests prove no dupe under concurrent pickup/equip/sell (run 100 parallel bot attempts on one drop); equipping weapon changes DPS in combat formula tests; vendor round-trip preserves ledger consistency (sum of ledger == world gold).

### M5 — Quests & Town
Build: quest framework (states, objectives kill/collect/talk, rewards), 15 quests, Havenport zone with all NPCs, quest UI (log, tracker, NPC dialog), breadcrumb flow field↔town.
**Accept:** bot completes the full 15-quest chain to level ~9 autonomously (this doubles as the master regression test — keep it in CI as `make questrun`); abandoning/retaking quests safe; rewards ledger-logged.

### M6 — Party, Friends, Trading, Auction
Build: party invite/leave/kick, shared XP + loot modes, party frames; friends list with online status; trade window (two-phase confirm, escrow); auction house (list, browse/search, buyout, expiry returns, 5% fee) with UI; dungeon-ready group plumbing.
**Accept:** 5-bot party clears a mob camp with correct XP split; trade fuzz test (random accept/cancel/disconnect mid-trade × 500) leaves zero item/gold discrepancies against ledger; auction expiry returns items; concurrent buyout race resolves to exactly one winner.

### M7 — Dungeon & Boss
Build: instanced Hollow Depths (per-party instance lifecycle, lockout), 3 rooms of elites, Warden Kryx with telegraphed AoE mechanic, dungeon quest chain, loot table with 2 "chase" items.
**Accept:** 5-bot party completes the dungeon; instance isolation test (two parties, no cross-visibility); lockout enforced; orphaned instances garbage-collected; boss AoE avoidable by a moving bot (mechanic actually dodgeable).

### M8 — GM Tools & Admin Dashboard
Build: all §8 GM commands + audit; full admin dashboard incl. TOTP, player management, health/economy panels, news editor, build publisher, drain/restart.
**Accept:** every GM command has a bot-verified effect + audit row; dashboard actions (kick/ban/grant) verified by bots; drain gracefully saves and disconnects 100 bots; non-GM cannot invoke any GM path (negative tests).

### M9 — Portal: Voting, Donations, Rankings, Downloads
Build: public portal pages, rankings cron, vote outlinks + signed postback crediting vote points, sandbox donation flow + manual approval queue in admin, vote/donation credit shop (cosmetic/QoL items via NPC or portal redemption), client download page fed by build publisher, server status widget.
**Accept:** simulated vote postback credits points exactly once (replay-protected); sandbox donation lifecycle (initiate → approve in admin → credits appear in-game); rankings match DB ground truth; download page serves the latest published build with correct sha256.

### M10 — Hardening, Polish & Release Candidate
Build: 200-bot soak (2 h) with mixed behavior profiles; crash recovery test (kill -9 gameserver mid-combat → restart → world consistent, ledger intact); backup/restore drill documented in runbook; client performance pass (60 fps medium on integrated GPU — profile and fix); asset polish pass (replace remaining placeholders per §9); onboarding polish (first 10 minutes of play); security self-review checklist in `/docs/security-review.md`.
**Accept:** 200-bot 2 h soak: 0 crashes, tick p99 < 50 ms, memory stable; restore-from-backup drill succeeds on a scratch DB; `make questrun` green; HUMAN sign-off playtest recorded in FEEDBACK.md.

---

## 12. Autonomous Dev Workflow Loop (how the agent must operate)

### 12.1 First actions on receiving this document
1. Create the repo, commit this file as `docs/BRIEF.md`.
2. Write `AGENTS.md` distilling: build/test commands, code conventions, the workflow loop below, and the guardrails from §10. Every future session starts by reading `AGENTS.md` + `docs/STATE.md`.
3. Create `docs/STATE.md` — the living status file: current milestone, task checklist, blockers, next action. Update it at the end of every work session. This is the agent's memory across sessions/context windows.
4. Begin M0.

### 12.2 The loop (repeat until MVP complete)
```
PLAN    → read STATE.md; pick the next unchecked task in the current milestone;
          break it into sub-tasks; note the test that will prove it works.
BUILD   → implement in small commits (conventional commits: feat/fix/test/refactor(scope): …).
          Server logic ALWAYS lands with unit tests. Protocol changes ALWAYS update shared/proto + both codegens + docs/protocol.md.
TEST    → make test (unit) → make bottest (protocol/integration via tools/botclient)
          → for world-behavior changes: make loadtest N=25 DURATION=5m.
          A failing test is fixed before anything new is started. Never delete or weaken
          a test to make it pass; never mark acceptance criteria done without the proof command output.
SHIP    → make deploy (rebuild, migrate, rolling restart with drain);
          make export-client when client changed → publish build via admin API so the
          download page always has the current build.
RECORD  → update STATE.md (done/next/blockers); append one line to docs/CHANGELOG.md;
          check FEEDBACK.md and HUMAN_TODO.md for new human input and fold it into the plan
          (human feedback outranks the default task order; the BRIEF outranks convenience).
```

### 12.3 Testing strategy (the agent's senses)
The agent cannot play the game, so bots are its eyes:
- **tools/botclient** speaks the real protobuf protocol end-to-end (register→login→play). Behavior profiles: `roamer`, `grinder`, `quester`, `trader`, `partygoer`, `chaos` (random valid+invalid packets — the fuzzer).
- Every milestone's acceptance criteria are encoded as bot scenarios under `tools/botclient/scenarios/` and wired into `make bottest`.
- `make questrun` (M5+) is the master regression: a bot playing the entire game start-to-finish. Run before every deploy.
- **chaos profile is mandatory from M2 onward** — malformed input must never crash the server.
- Client-side: `godot --headless` script tests for protocol/session/UI-model code; rendering is verified by the human.

### 12.4 Human-in-the-loop protocol (the laptop playtest cycle)
- The human periodically downloads the current client from the portal (or `scp`), plays against the live VPS server, and writes observations into `FEEDBACK.md` in the repo (agent never overwrites it; it appends `-> resolved:` notes under items).
- Cadence: expect a human playtest at least at the end of M2, M3, M5, M7, and M10 — these are the "does it feel right" gates that bots can't judge (camera, animation timing, combat feel, jank).
- `HUMAN_TODO.md` collects things only the human can do: supply VRoid models, register the domain, provide real payment keys, pick the final game name, create GM accounts' emails, choose topsite listings.
- If truly blocked on a human item, park it, place placeholders, continue with the next task. Never idle.

### 12.5 Git & release discipline
- `main` is always deployable. Work in short-lived branches per task, merge on green tests.
- Tag `m<N>-complete` at each milestone; keep binary client builds out of git (publish to `/var/aetheria/builds`, tracked in the `client_builds` table).
- Nightly `deploy/backup.sh`: pg_dump + content + builds → compressed, rotated 14 days, optionally rclone'd off-box (HUMAN_TODO: provide off-site target).

### 12.6 Ops guardrails on the shared VPS
- The agent shares the box with the live server: build with `nice`/`ionice`; never run loadtests > 50 bots against the *live* process while human players are online — use the staging instance (`gameserver -config staging.yaml`, ports 7101+, separate `aetheria_staging` DB).
- Deploys drain gracefully (stop accepting logins, save-all, 60 s notice broadcast).
- Watchdog: systemd `Restart=on-failure` + a health-check timer that alerts (writes to STATE.md and logs) if any service flaps.

---

## 13. Definition of Done for the whole MVP
All M0–M10 acceptance criteria green · `make questrun` passes · 200-bot 2 h soak passes · human sign-off in FEEDBACK.md · runbook complete · CREDITS.md covers every asset · no HUMAN_TODO items blocking play · a stranger can go from portal registration to killing Warden Kryx with a party using only what the portal provides.

---

## 14. Post-MVP backlog (for context only — do NOT build now)
Guilds, PvP (duels → zone → battlegrounds), crafting/gathering, mounts/pets, mail, bank, more classes/races/zones, character customization depth, real payment providers, launcher with auto-update, seasonal events, localization, macOS export, sharding/multi-zone processes if CCU outgrows one box.
