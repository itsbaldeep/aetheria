#!/usr/bin/env bash
# deploy.sh — build, migrate, rolling restart of all four Aetheria services.
# Run as: make deploy (from repo root). Safe to re-run (idempotent).
set -euo pipefail
cd "$(dirname "$0")/.."

ENV_FILE="${AETHERIA_ENV_FILE:-$HOME/aetheria/env}"
[ -f "$ENV_FILE" ] || { echo "deploy: missing env file $ENV_FILE"; exit 1; }

echo "==> building images (nice)"
nice -n 10 docker compose -f deploy/docker-compose.yml build

echo "==> applying migrations"
make migrate

echo "==> rolling restart"
# Drain semantics for gameserver would come via its control endpoint (M8);
# for M0 a plain recreate is sufficient and zero-data-loss (no world state yet).
nice -n 10 docker compose -f deploy/docker-compose.yml up -d

echo "==> health"
for svc in authserver gameserver adminserver portal; do
  port=$(grep "^AETHERIA_${svc^^}_PORT" "$ENV_FILE" 2>/dev/null | cut -d= -f2 || true)
  [ -z "$port" ] && port="0000"
  # map container names to env keys
  case "$svc" in
    authserver) port=$(grep '^AETHERIA_AUTH_PORT' "$ENV_FILE" | cut -d= -f2);;
    gameserver) port=$(grep '^AETHERIA_GAME_PORT' "$ENV_FILE" | cut -d= -f2);;
    adminserver) port=$(grep '^AETHERIA_ADMIN_PORT' "$ENV_FILE" | cut -d= -f2);;
    portal) port=$(grep '^AETHERIA_PORTAL_PORT' "$ENV_FILE" | cut -d= -f2);;
  esac
  if curl -fsS "http://127.0.0.1:$port/healthz" >/dev/null 2>&1; then
    echo "  $svc: OK"
  else
    echo "  $svc: FAIL (port $port)"
    exit 1
  fi
done
echo "==> deploy complete"
