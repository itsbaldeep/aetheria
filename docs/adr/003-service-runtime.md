# ADR-003: Service Runtime — Docker host-network instead of bare systemd

Status: accepted (2026-08-11)

## Context
The brief (§3) says the four Go services run "under systemd". The agent runs
as the non-root `agency` user on a shared VPS with a fixed sudo allowlist
(`sudo -l` shows only caddy reload / iptables / two systemctl units). It
cannot write `/etc/aetheria/env` or install systemd units, and the agency
doctrine (`~/projects/AGENTS.md` §2) mandates Docker-with-memory-limits for
every project service.

## Decision
Run the four Go services as **Docker containers with `network_mode: host`**
via `deploy/docker-compose.yml`, each with a memory limit (gameserver 4 g,
portal 512 m, auth/admin 256 m). Postgres + Redis are their own long-lived
containers (ADR-001). Secrets live at `~/aetheria/env` (600), loaded via
`env_file`, never in the repo.

Host networking means:
- Each service binds `127.0.0.1:<allocated port>` (ADR-001) — reachable by
  Caddy on the host, invisible to the outside world (TLS terminates at Caddy).
- No port publishing = no accidental exposure of game ports.
- `restart: unless-stopped` + `deploy.resources.limits.memory` gives the same
  supervision guarantees systemd would.

The `deploy/systemd/*.unit` files are still provided for a future privileged
migration (when the human grants root or Aetheria moves to its own box).

## Consequences
- Services survive host reboot (docker restart policy) and are memory-capped.
- `make deploy` = `docker compose build + up -d` + `make migrate`.
- No root needed; stays inside the sudo allowlist.
- Deviates from the brief's literal "systemd"; supervision semantics preserved.
