#!/bin/sh
# Online SQLite backup, keeping 14 days. Optional: BACKUP_GCS=gs://bucket/path
# copies each backup off the VM with gsutil.
set -eu
DB="${DB_PATH:-data/findbugs.db}"
DIR="${BACKUP_DIR:-data/backups}"
mkdir -p "$DIR"
chmod 700 "$DIR"
OUT="$DIR/findbugs-$(date +%Y%m%d-%H%M%S).db"
sqlite3 "$DB" ".backup '$OUT'"
chmod 600 "$OUT"
gzip "$OUT"
find "$DIR" -name 'findbugs-*.db.gz' -mtime +14 -delete
if [ -n "${BACKUP_GCS:-}" ]; then
	gsutil -q cp "$OUT.gz" "$BACKUP_GCS/"
fi
echo "backup written: $OUT.gz"
