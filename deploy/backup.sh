#!/usr/bin/env bash
# backup.sh — nightly backup of Aetheria state: pg_dump + content + builds.
# Rotates 14 days. Off-site target = HUMAN_TODO (M10).
set -euo pipefail

BACKUP_ROOT="${AETHERIA_BACKUP_ROOT:-$HOME/aetheria/backups}"
KEEP_DAYS=14
mkdir -p "$BACKUP_ROOT"
TS=$(date +%Y%m%d-%H%M%S)
DEST="$BACKUP_ROOT/aetheria-$TS"

echo "==> pg_dump"
docker exec aetheria-postgres pg_dump -U aetheria -d aetheria > "$DEST.sql" 2>/dev/null || {
  # fallback direct dump via env creds
  source "$HOME/aetheria/env"
  PGPASSWORD="$AETHERIA_PG_PASSWORD" pg_dump -h 100.64.0.1 -p "$AETHERIA_PG_PORT" -U aetheria -d aetheria > "$DEST.sql"
}

echo "==> content + docs"
tar -czf "$DEST-content.tar.gz" -C "$HOME/projects/aetheria" shared/content docs 2>/dev/null || true

echo "==> builds (if any)"
if [ -d /var/aetheria/builds ]; then
  tar -czf "$DEST-builds.tar.gz" -C /var/aetheria/builds .
fi

echo "==> prune > $KEEP_DAYS days"
find "$BACKUP_ROOT" -name 'aetheria-*' -mtime "+$KEEP_DAYS" -delete

echo "backup complete: $DEST"
