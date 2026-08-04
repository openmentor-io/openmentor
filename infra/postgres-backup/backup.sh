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
# A third marker, .first_start, records when the daemon first ran against this
# volume and is never rewritten. It exists so a brand-new volume gets one grace
# window before the healthcheck and the alert fire, WITHOUT pretending a backup
# succeeded: .last_success (and its gauge) stay absent/0 until one really does.
#
# BACKUP_MAX_AGE_HOURS itself is published as openmentor_db_backup_max_age_seconds
# so the staleness alert compares against the window this container actually
# enforces rather than a copy of it (grafana/alerting/alert-rules.yaml).
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
FIRST_START_MARKER="${BACKUP_DIR}/.first_start"
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

# Export the markers as Prometheus gauges for Alloy's textfile collector.
# Written via a temp file + rename so a scrape never sees a partial file.
#
# Best-effort on purpose: publishing metrics must never turn a backup that
# actually succeeded into a reported failure. An unwritable metrics dir loses
# the gauges, which the staleness alert then catches on its own.
write_metrics() {
    mkdir -p "$BACKUP_METRICS_DIR" 2>/dev/null || true
    # Plain if/else, not `if ! ...`: bash reports a compound command whose
    # redirection failed as successful, so `if !` takes the else branch there and
    # misses exactly this case (ash/dash do not). This form works in every shell.
    if {
        echo "# HELP openmentor_db_backup_last_success_timestamp_seconds Unix time of the last successful pg_dump (plus S3 upload when a bucket is configured). 0 = never."
        echo "# TYPE openmentor_db_backup_last_success_timestamp_seconds gauge"
        echo "openmentor_db_backup_last_success_timestamp_seconds $(marker_epoch "$SUCCESS_MARKER")"
        echo "# HELP openmentor_db_backup_last_failure_timestamp_seconds Unix time of the last failed backup attempt. 0 = never."
        echo "# TYPE openmentor_db_backup_last_failure_timestamp_seconds gauge"
        echo "openmentor_db_backup_last_failure_timestamp_seconds $(marker_epoch "$FAILURE_MARKER")"
        echo "# HELP openmentor_db_backup_first_start_timestamp_seconds Unix time the backup daemon first ran against this volume, never rewritten. Start of the one grace window allowed before a never-successful pipeline is alerted on. 0 = the daemon has never started."
        echo "# TYPE openmentor_db_backup_first_start_timestamp_seconds gauge"
        echo "openmentor_db_backup_first_start_timestamp_seconds $(marker_epoch "$FIRST_START_MARKER")"
        # The window this container enforces, so the DatabaseBackupStale alert can
        # compare against it instead of hardcoding one: raising
        # BACKUP_MAX_AGE_HOURS in .env.production would otherwise leave the alert
        # paging at the old threshold (and lowering it would page late).
        echo "# HELP openmentor_db_backup_max_age_seconds Configured freshness window (BACKUP_MAX_AGE_HOURS) the healthcheck and the staleness alert enforce."
        echo "# TYPE openmentor_db_backup_max_age_seconds gauge"
        echo "openmentor_db_backup_max_age_seconds $(( BACKUP_MAX_AGE_HOURS * 3600 ))"
    } > "${METRICS_FILE}.tmp" 2>/dev/null; then
        mv "${METRICS_FILE}.tmp" "$METRICS_FILE" 2>/dev/null ||
            log "WARNING: cannot replace ${METRICS_FILE} - the backup gauges will go stale."
    else
        rm -f "${METRICS_FILE}.tmp" 2>/dev/null || true
        log "WARNING: cannot write ${METRICS_FILE} - the backup gauges will go" \
            "stale and DatabaseBackupStale will fire on NoData. Is" \
            "${BACKUP_METRICS_DIR} full or mounted read-only?"
    fi
    return 0
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
        now=$(date -u +%s)
        max=$(( BACKUP_MAX_AGE_HOURS * 3600 ))
        last_success=$(marker_epoch "$SUCCESS_MARKER")
        if [ "$last_success" -eq 0 ]; then
            # Nothing has ever succeeded. Stay healthy for one window measured
            # from the daemon's first start on this volume, then say so.
            waiting=$(( now - $(marker_epoch "$FIRST_START_MARKER") ))
            if [ -f "$FIRST_START_MARKER" ] && [ "$waiting" -le "$max" ]; then
                echo "healthy: no backup yet, ${waiting}s into the ${max}s grace window since first start"
                exit 0
            fi
            echo "unhealthy: no backup has ever succeeded (${SUCCESS_MARKER} missing)" >&2
            exit 1
        fi
        age=$(( now - last_success ))
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
        # First start against this /backups volume: stamp .first_start, which
        # opens the one grace window the healthcheck and the staleness alert
        # honour before a never-successful pipeline counts as broken. Written
        # only when absent and never touched again, so a redeploy cannot reset
        # the window on a pipeline that has been failing for weeks - and,
        # unlike seeding .last_success, it does not claim a backup happened.
        if [ ! -f "$FIRST_START_MARKER" ]; then
            date -u +%s > "$FIRST_START_MARKER"
            log "first start against ${BACKUP_DIR} - no backup has succeeded" \
                "yet, so the next ${BACKUP_MAX_AGE_HOURS}h are a grace window;" \
                "after that a missing dump turns this container unhealthy"
        fi
        write_metrics
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
