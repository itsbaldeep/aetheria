# Aetheria CHANGELOG

Every work block appends exactly one line here (scope: description).

- 2026-08-11 (M0): repo + ledger + ADR-001/002; toolchain installed; AGENTS.md + STATE.md scaffold
- 2026-08-11 (M0): 4 subdomains live over TLS; m0-complete tagged
- 2026-08-11 (M1): portal registration (argon2id, validation, Redis rate-limit, hash semaphore); authserver /auth/register + /auth/login; bot register scenario 20/20 green
- 2026-08-11 (M1): login issues HS256 JWT (24 h TTL, env key); logins audited; bot login profile green (token + 401/403 paths)
- 2026-08-11 (M1): character create/list endpoints (name rules, 2 classes, roster cap, Bearer auth); bot create-char profile green (401/400/201/409 + roster)
- 2026-08-11 (M1): client Login + CharSelect scenes, Session/ClientConfig/ApiClient layer (headless-tested), config.json next to exe; live Godot register→login→roster green
- 2026-08-11 (M1): gameserver WS handshake validates Bearer session + ban re-check before ServerHello; bot full-auth profile = M1 acceptance (register→login→create char→authed WS hello + reject paths) ALL PASS live; m1 acceptance met
- 2026-08-11 (M2): world sim (20 Hz, AOI 30 m cells, speed/bounds clamps), wire EnterWorld/MoveIntent/LeaveWorld single-writer outbox + Player.Ready ack gating, auth spawn load/save, zones + /control/ccu; botclient WorldBot/presence/roamer/chaos profiles ALL PASS live; M2 acceptance met
- 2026-08-12 (M3): combat core live — mobs/bands/skills/aggro/leash, XP + death + respawn at shrine; position-derived zone bounds (havenport pocket in emberfield, walking-out transition) + deterministic spawns; bot combat scenario (kill boar +12 XP → pull Ashmaw → die → respawn HP=100) ALL PASS; bottest runs combat
