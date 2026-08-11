# ADR-001: Port Allocation & Bind Strategy

Status: accepted (2026-08-11)

## Context
The brief (§3) specifies fixed ports 7000–7011. The Agency OS doctrine
(`~/projects/AGENTS.md` §11) mandates: all project ports come from
`orch port alloc`, public services live in 3000–3999, internal services in
5000–5999, and the control-plane ports (5432/8123/9000/4096/5001/8080/9999)
are protected. Caddy terminates TLS and proxies to localhost.

## Decision
Allocate via `orch port alloc aetheria <service>` and record the mapping in
`deploy/env`. No service binds a fixed port; everything is config-driven
(`AETHERIA_*_PORT` env vars loaded by each binary).

| Service | Env var | Port | Range | Bind | Exposure |
|---|---|---|---|---|---|
| authserver | `AETHERIA_AUTH_PORT` | 3016 | 3000s | 127.0.0.1 | Caddy → api.aetheria.apps.deployden.tech |
| gameserver | `AETHERIA_GAME_PORT` | 3015 | 3000s | 0.0.0.0 | Caddy wss → play.aetheria.apps.deployden.tech/ws |
| adminserver | `AETHERIA_ADMIN_PORT` | 3017 | 3000s | 127.0.0.1 | Caddy → admin.aetheria.apps.deployden.tech |
| portal | `AETHERIA_PORTAL_PORT` | 3018 | 3000s | 127.0.0.1 | Caddy → aetheria.apps.deployden.tech |
| gameserver control | `AETHERIA_CONTROL_PORT` | 5003 | 5000s | 127.0.0.1 | localhost-only (adminserver→gameserver) |
| postgres | `AETHERIA_PG_PORT` | 5004 | 5000s | 100.64.0.1 | project containers only (VPN) |
| redis | `AETHERIA_REDIS_PORT` | 5005 | 5000s | 100.64.0.1 | project containers only (VPN) |

## Consequences
- The brief's "localhost:5432" / "localhost:6379" / ":7000-7011" are replaced
  by these allocated values, avoiding every collision with the control plane.
- Client `config.json` points at the public wss URL, never these internal ports.
- Renaming to a custom domain is a branding/config change only (ADR-002).
