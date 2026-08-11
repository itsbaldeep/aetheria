# Aetheria CHANGELOG

Every work block appends exactly one line here (scope: description).

- 2026-08-11 (M0): repo + ledger + ADR-001/002; toolchain installed; AGENTS.md + STATE.md scaffold
- 2026-08-11 (M0): 4 subdomains live over TLS; m0-complete tagged
- 2026-08-11 (M1): portal registration (argon2id, validation, Redis rate-limit, hash semaphore); authserver /auth/register + /auth/login; bot register scenario 20/20 green
- 2026-08-11 (M1): login issues HS256 JWT (24 h TTL, env key); logins audited; bot login profile green (token + 401/403 paths)
