#!/bin/bash
# VigilAgent Database Backup Script
# Usage: ./scripts/backup-db.sh [daily|weekly|monthly]

set -euo pipefail

# Configuration
BACKUP_DIR="${BACKUP_DIR:-./backups}"
DB_HOST="${DB_HOST:-localhost}"
DB_PORT="${DB_PORT:-5432}"
DB_NAME="${DB_NAME:-vigilagent}"
DB_USER="${DB_USER:-vigilagent}"
RETENTION_DAYS="${RETENTION_DAYS:-30}"
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_TYPE="${1:-daily}"

# Create backup directory
mkdir -p "$BACKUP_DIR/$BACKUP_TYPE"

# Generate backup filename
BACKUP_FILE="$BACKUP_DIR/$BACKUP_TYPE/vigilagent_${BACKUP_TYPE}_${TIMESTAMP}.sql.gz"

echo "Starting $BACKUP_TYPE backup..."
echo "Database: $DB_NAME"
echo "Output: $BACKUP_FILE"

# Run pg_dump with compression
PGPASSWORD="${DB_PASSWORD:-}" pg_dump \
  -h "$DB_HOST" \
  -p "$DB_PORT" \
  -U "$DB_USER" \
  -d "$DB_NAME" \
  --format=custom \
  --compress=9 \
  --verbose \
  2>/dev/null | gzip > "$BACKUP_FILE"

# Verify backup
if [ -f "$BACKUP_FILE" ] && [ -s "$BACKUP_FILE" ]; then
  BACKUP_SIZE=$(du -h "$BACKUP_FILE" | cut -f1)
  echo "✅ Backup completed successfully!"
  echo "   File: $BACKUP_FILE"
  echo "   Size: $BACKUP_SIZE"
else
  echo "❌ Backup failed!"
  exit 1
fi

# Cleanup old backups
echo "Cleaning up backups older than $RETENTION_DAYS days..."
find "$BACKUP_DIR/$BACKUP_TYPE" -name "*.sql.gz" -mtime +$RETENTION_DAYS -delete 2>/dev/null || true

echo "✅ Backup process complete!"
