#!/bin/sh
# ============================================================================
# Nightly PostgreSQL logical backups for openmentor.io (DECISIONS D2)
# ============================================================================
# Runs as the `postgres-backup` compose sidecar. Once a day (BACKUP_TIME,
# HH:MM UTC, default 03:30) it takes a pg_dump of $POSTGRES_DB in custom
# format (-Fc, already compressed, restorable with pg_restore) named
# openmentor-YYYYMMDD-HHMM.dump and then:
#
#   - BACKUP_S3_BUCKET set   -> uploads to s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/
#                               and prunes S3 objects older than
#                               BACKUP_RETENTION_DAYS (default 30).
#   - BACKUP_S3_BUCKET unset -> keeps the dump in the local /backups volume
#                               (openmentor-postgres-backups) with the same
#                               retention and logs a loud warning: local-only
#                               backups die with the VM.
#
# S3 credentials: BACKUP_AWS_ACCESS_KEY_ID / BACKUP_AWS_SECRET_ACCESS_KEY
# take precedence; when empty we fall back to the backend's S3_STORAGE_*
# keys (same AWS account per DECISIONS D15). Prefer a dedicated IAM user
# scoped to the backup bucket.
#
# Usage: backup.sh [daemon|once]
#   daemon  loop forever, one backup per day at BACKUP_TIME (default)
#   once    run a single backup immediately and exit (manual/drill runs:
#           docker exec openmentor-postgres-backup backup.sh once)
#
# Restore procedure: ../docs/runbooks/postgres-backup-restore.md (docs repo).
# NOTE: must stay busybox-ash compatible (no bashisms).
# ============================================================================
set -eu

POSTGRES_HOST="${POSTGRES_HOST:-postgres}"
POSTGRES_USER="${POSTGRES_USER:-openmentor}"
POSTGRES_DB="${POSTGRES_DB:-openmentor}"
BACKUP_TIME="${BACKUP_TIME:-03:30}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-30}"
BACKUP_S3_BUCKET="${BACKUP_S3_BUCKET:-}"
BACKUP_S3_PREFIX="${BACKUP_S3_PREFIX:-postgres}"
BACKUP_DIR="${BACKUP_DIR:-/backups}"

export PGPASSWORD="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"

# Dedicated backup credentials first, S3_STORAGE_* fallback second (D15)
AWS_ACCESS_KEY_ID="${BACKUP_AWS_ACCESS_KEY_ID:-${S3_STORAGE_ACCESS_KEY:-}}"
AWS_SECRET_ACCESS_KEY="${BACKUP_AWS_SECRET_ACCESS_KEY:-${S3_STORAGE_SECRET_KEY:-}}"
AWS_DEFAULT_REGION="${BACKUP_AWS_REGION:-${S3_STORAGE_REGION:-eu-central-1}}"
export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_DEFAULT_REGION

log() {
    echo "[postgres-backup] $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"
}

warn_local_only() {
    log "WARNING: BACKUP_S3_BUCKET is not set - dumps stay ONLY in the local" \
        "'openmentor-postgres-backups' volume and are LOST if the VM dies." \
        "Set BACKUP_S3_BUCKET for off-site backups."
}

# Seconds until the next occurrence of $1 (HH:MM, UTC)
seconds_until() {
    hh="${1%%:*}"
    mm="${1#*:}"
    hh="${hh#0}"
    mm="${mm#0}"
    target=$(( ${hh:-0} * 3600 + ${mm:-0} * 60 ))
    now=$(( $(date -u +%s) % 86400 ))
    diff=$(( target - now ))
    if [ "$diff" -le 0 ]; then
        diff=$(( diff + 86400 ))
    fi
    echo "$diff"
}

# Retention cutoff as a YYYYMMDDHHMM number (compare against the stamp
# embedded in dump filenames)
cutoff_num() {
    date -u -d "@$(( $(date -u +%s) - BACKUP_RETENTION_DAYS * 86400 ))" +%Y%m%d%H%M
}

# Delete S3 dumps older than the retention window; prints how many
prune_s3() {
    cutoff=$(cutoff_num)
    count=0
    for key in $(aws s3 ls "s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/" | awk '{print $4}'); do
        case "$key" in
            openmentor-*.dump) ;;
            *) continue ;;
        esac
        stamp="${key#openmentor-}"
        stamp="${stamp%.dump}"
        num=$(echo "$stamp" | tr -d '-')
        case "$num" in
            *[!0-9]* | "") continue ;;
        esac
        if [ "$num" -lt "$cutoff" ]; then
            aws s3 rm "s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/${key}" --only-show-errors
            count=$(( count + 1 ))
        fi
    done
    echo "$count"
}

# Delete local dumps older than the retention window; prints how many
prune_local() {
    find "$BACKUP_DIR" -name 'openmentor-*.dump' -type f \
        -mtime "+${BACKUP_RETENTION_DAYS}" -print -delete | wc -l | tr -d ' '
}

run_backup() {
    stamp=$(date -u +%Y%m%d-%H%M)
    file="openmentor-${stamp}.dump"
    path="${BACKUP_DIR}/${file}"

    # Custom format (-Fc) is compressed by pg_dump itself - no gzip needed
    if ! pg_dump -h "$POSTGRES_HOST" -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc -f "$path"; then
        rm -f "$path"
        log "FAILURE db=${POSTGRES_DB} file=${file} error=pg_dump_failed"
        return 1
    fi
    size=$(du -h "$path" | cut -f1)

    if [ -n "$BACKUP_S3_BUCKET" ]; then
        dest="s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/${file}"
        if ! aws s3 cp "$path" "$dest" --only-show-errors; then
            log "FAILURE db=${POSTGRES_DB} file=${file} error=s3_upload_failed (dump kept at ${path})"
            return 1
        fi
        rm -f "$path"
        pruned=$(prune_s3)
        log "SUCCESS db=${POSTGRES_DB} file=${file} size=${size} dest=${dest} pruned=${pruned} retention_days=${BACKUP_RETENTION_DAYS}"
    else
        warn_local_only
        pruned=$(prune_local)
        log "SUCCESS db=${POSTGRES_DB} file=${file} size=${size} dest=${path} pruned=${pruned} retention_days=${BACKUP_RETENTION_DAYS}"
    fi
}

mkdir -p "$BACKUP_DIR"

case "${1:-daemon}" in
    once)
        run_backup
        ;;
    daemon)
        log "starting: daily pg_dump of '${POSTGRES_DB}' at ${BACKUP_TIME} UTC," \
            "retention ${BACKUP_RETENTION_DAYS} days," \
            "destination $([ -n "$BACKUP_S3_BUCKET" ] && echo "s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/" || echo "${BACKUP_DIR} (local volume)")"
        if [ -z "$BACKUP_S3_BUCKET" ]; then
            warn_local_only
        fi
        while true; do
            wait_s=$(seconds_until "$BACKUP_TIME")
            log "next backup in ${wait_s}s"
            sleep "$wait_s"
            run_backup || true
            # Guard against re-firing within the same minute
            sleep 60
        done
        ;;
    *)
        echo "usage: backup.sh [daemon|once]" >&2
        exit 1
        ;;
esac
