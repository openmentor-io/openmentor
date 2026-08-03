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
# S3 credentials: BACKUP_AWS_ACCESS_KEY_ID / BACKUP_AWS_SECRET_ACCESS_KEY are
# REQUIRED whenever BACKUP_S3_BUCKET is set — there is NO fallback to the
# backend's S3_STORAGE_* keys (removed by SECURITY M12, see below), and this
# script exits 1 without them. Use a dedicated IAM user scoped to the backup
# bucket.
#
# Every successful run refreshes $BACKUP_DIR/.last_success (and a failure
# refreshes .last_failure); `backup.sh healthcheck` — the compose healthcheck —
# fails once the success marker ages past BACKUP_MAX_AGE_HOURS. Both markers
# are also exported as Prometheus gauges into BACKUP_METRICS_DIR, which Alloy
# scrapes with its textfile collector. That chain is the ONLY signal that an
# unattended nightly dump has stopped working: the daemon loop deliberately
# swallows failures so a transient error doesn't kill the sidecar.
#
# Usage: backup.sh [daemon|once|healthcheck]
#   daemon       loop forever, one backup per day at BACKUP_TIME (default)
#   once         run a single backup immediately and exit (manual/drill runs:
#                docker exec openmentor-postgres-backup backup.sh once)
#   healthcheck  exit 0 if the last success is younger than
#                BACKUP_MAX_AGE_HOURS, else 1 (used by compose)
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
# Freshness window the compose healthcheck enforces: a bit over one daily
# interval, so a single late run is not flagged but a skipped night is.
BACKUP_MAX_AGE_HOURS="${BACKUP_MAX_AGE_HOURS:-26}"
# Prometheus textfile-collector directory, shared read-only with Alloy
BACKUP_METRICS_DIR="${BACKUP_METRICS_DIR:-/var/lib/backup-metrics}"

SUCCESS_MARKER="${BACKUP_DIR}/.last_success"
FAILURE_MARKER="${BACKUP_DIR}/.last_failure"
METRICS_FILE="${BACKUP_METRICS_DIR}/openmentor_db_backup.prom"

export PGPASSWORD="${POSTGRES_PASSWORD:?POSTGRES_PASSWORD is required}"

# SECURITY (M12): use ONLY dedicated backup credentials. The previous fallback
# to the app's S3_STORAGE_* keys meant a compromised app key could delete both
# the profile images AND every DB backup (this script runs `aws s3 rm`). The
# backup credentials should be a separate IAM identity without s3:DeleteObject
# on the app bucket; pair with bucket versioning / Object Lock on the backup
# bucket. When BACKUP_S3_BUCKET is set, the dedicated creds are required.
AWS_ACCESS_KEY_ID="${BACKUP_AWS_ACCESS_KEY_ID:-}"
AWS_SECRET_ACCESS_KEY="${BACKUP_AWS_SECRET_ACCESS_KEY:-}"
AWS_DEFAULT_REGION="${BACKUP_AWS_REGION:-eu-central-1}"
export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_DEFAULT_REGION

if [ -n "${BACKUP_S3_BUCKET}" ] && { [ -z "${AWS_ACCESS_KEY_ID}" ] || [ -z "${AWS_SECRET_ACCESS_KEY}" ]; }; then
    echo "[postgres-backup] FATAL: BACKUP_S3_BUCKET is set but BACKUP_AWS_ACCESS_KEY_ID / BACKUP_AWS_SECRET_ACCESS_KEY are not." >&2
    echo "[postgres-backup] Provide dedicated backup credentials (no fallback to app S3 keys). Refusing to start." >&2
    exit 1
fi

log() {
    echo "[postgres-backup] $(date -u '+%Y-%m-%dT%H:%M:%SZ') $*"
}

warn_local_only() {
    log "WARNING: BACKUP_S3_BUCKET is not set - dumps stay ONLY in the local" \
        "'openmentor-postgres-backups' volume and are LOST if the VM dies." \
        "Set BACKUP_S3_BUCKET for off-site backups."
}

# Epoch stored in $1, or 0 when the marker does not exist yet
marker_epoch() {
    if [ -f "$1" ]; then
        cat "$1"
    else
        echo 0
    fi
}

# Export both markers as Prometheus gauges for Alloy's textfile collector.
# Written via a temp file + rename so a scrape never sees a partial file.
write_metrics() {
    mkdir -p "$BACKUP_METRICS_DIR" 2>/dev/null || return 0
    {
        echo "# HELP openmentor_db_backup_last_success_timestamp_seconds Unix time of the last successful pg_dump (plus S3 upload when a bucket is configured). 0 = never."
        echo "# TYPE openmentor_db_backup_last_success_timestamp_seconds gauge"
        echo "openmentor_db_backup_last_success_timestamp_seconds $(marker_epoch "$SUCCESS_MARKER")"
        echo "# HELP openmentor_db_backup_last_failure_timestamp_seconds Unix time of the last failed backup attempt. 0 = never."
        echo "# TYPE openmentor_db_backup_last_failure_timestamp_seconds gauge"
        echo "openmentor_db_backup_last_failure_timestamp_seconds $(marker_epoch "$FAILURE_MARKER")"
    } > "${METRICS_FILE}.tmp" && mv "${METRICS_FILE}.tmp" "$METRICS_FILE"
}

mark_success() {
    date -u +%s > "$SUCCESS_MARKER"
    write_metrics
}

mark_failure() {
    date -u +%s > "$FAILURE_MARKER"
    write_metrics
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
        # Prune anyway: an S3 upload that failed on an earlier run left a
        # full-size dump behind, and nothing else ever removes it.
        prune_local >/dev/null
        mark_failure
        log "FAILURE db=${POSTGRES_DB} file=${file} error=pg_dump_failed"
        return 1
    fi
    size=$(du -h "$path" | cut -f1)

    if [ -n "$BACKUP_S3_BUCKET" ]; then
        dest="s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/${file}"
        if ! aws s3 cp "$path" "$dest" --only-show-errors; then
            # The dump stays local so a later manual `aws s3 cp` can ship it,
            # but it still ages out of the retention window - otherwise every
            # failed upload adds one full-size dump to the disk holding the
            # live database, forever.
            pruned=$(prune_local)
            mark_failure
            log "FAILURE db=${POSTGRES_DB} file=${file} error=s3_upload_failed (dump kept at ${path}, pruned=${pruned})"
            return 1
        fi
        rm -f "$path"
        pruned=$(prune_s3)
        # Kept dumps from earlier upload failures live here, not in S3
        prune_local >/dev/null
        mark_success
        log "SUCCESS db=${POSTGRES_DB} file=${file} size=${size} dest=${dest} pruned=${pruned} retention_days=${BACKUP_RETENTION_DAYS}"
    else
        warn_local_only
        pruned=$(prune_local)
        mark_success
        log "SUCCESS db=${POSTGRES_DB} file=${file} size=${size} dest=${path} pruned=${pruned} retention_days=${BACKUP_RETENTION_DAYS}"
    fi
}

mkdir -p "$BACKUP_DIR"

case "${1:-daemon}" in
    once)
        run_backup
        ;;
    healthcheck)
        age=$(( $(date -u +%s) - $(marker_epoch "$SUCCESS_MARKER") ))
        max=$(( BACKUP_MAX_AGE_HOURS * 3600 ))
        if [ ! -f "$SUCCESS_MARKER" ]; then
            echo "unhealthy: no backup has ever succeeded (${SUCCESS_MARKER} missing)" >&2
            exit 1
        fi
        if [ "$age" -gt "$max" ]; then
            echo "unhealthy: last successful backup was ${age}s ago (max ${max}s)" >&2
            exit 1
        fi
        echo "healthy: last successful backup ${age}s ago"
        ;;
    daemon)
        log "starting: daily pg_dump of '${POSTGRES_DB}' at ${BACKUP_TIME} UTC," \
            "retention ${BACKUP_RETENTION_DAYS} days," \
            "destination $([ -n "$BACKUP_S3_BUCKET" ] && echo "s3://${BACKUP_S3_BUCKET}/${BACKUP_S3_PREFIX}/" || echo "${BACKUP_DIR} (local volume)")"
        if [ -z "$BACKUP_S3_BUCKET" ]; then
            warn_local_only
        fi
        # First start on a fresh /backups volume: seed the success marker so
        # the healthcheck and the staleness alert get one grace window instead
        # of firing before the first scheduled dump can run. Only when absent -
        # seeding on every start would let a redeploy silently reset the window
        # on a pipeline that has been failing for weeks.
        if [ ! -f "$SUCCESS_MARKER" ]; then
            log "no ${SUCCESS_MARKER} yet - seeding it, so the first" \
                "${BACKUP_MAX_AGE_HOURS}h are a grace window; after that a" \
                "missing dump turns this container unhealthy"
            mark_success
        else
            write_metrics
        fi
        while true; do
            wait_s=$(seconds_until "$BACKUP_TIME")
            log "next backup in ${wait_s}s"
            sleep "$wait_s"
            # Deliberately swallowed: a transient failure must not kill the
            # daemon. mark_failure + the healthcheck + the staleness alert are
            # what make it visible.
            run_backup || true
            # Guard against re-firing within the same minute
            sleep 60
        done
        ;;
    *)
        echo "usage: backup.sh [daemon|once|healthcheck]" >&2
        exit 1
        ;;
esac
