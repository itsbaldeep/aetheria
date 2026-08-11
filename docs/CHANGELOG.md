# Aetheria CHANGELOG

Every work block appends exactly one line here (scope: description).

- 2026-08-11 (M0): repo + ledger + ADR-001/002; toolchain installed; AGENTS.md + STATE.md scaffold
- 2026-08-11 (M0): 4 subdomains live over TLS; m0-complete tagged
- 2026-08-11 (M1): portal registration (argon2id, validation, Redis rate-limit, hash semaphore); authserver /auth/register + /auth/login; bot register scenario 20/20 green
- 2026-08-11 (M1): login issues HS256 JWT (24 h TTL, env key); logins audited; bot login profile green (token + 401/403 paths)
- 2026-08-11 (M1): character create/list endpoints (name rules, 2 classes, roster cap, Bearer auth); bot create-char profile green (401/400/201/409 + roster)
- 2026-08-11 (M1): client Login + CharSelect scenes, Session/ClientConfig/ApiClient layer (headless-tested), config.json next to exe; live Godot register→login→roster green
