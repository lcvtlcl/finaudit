#!/usr/bin/env bash
set -euo pipefail
BACKUP_DIR="${BACKUP_DIR:-$HOME/finaudit-backups}"
mkdir -p "$BACKUP_DIR"
STAMP=$(date +%Y%m%d_%H%M%S)
OUT="$BACKUP_DIR/finaudit_$STAMP.sql.gz"
docker compose -f deploy/docker-compose.yml exec -T postgres pg_dump -U finaudit finaudit | gzip > "$OUT"
echo "backup: $OUT"
ls -1t "$BACKUP_DIR"/finaudit_*.sql.gz | tail -n +15 | xargs -r rm --
