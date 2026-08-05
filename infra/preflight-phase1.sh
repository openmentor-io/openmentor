#!/usr/bin/env bash
# ============================================================================
# Phase 1 preflight — will cmd/api, cmd/worker and cmd/migrate still START?
# ============================================================================
# PR #76 (audit H14, DECISIONS D62) replaces the one shared config.Validate()
# with a per-binary contract: config.LoadFor(BinaryAPI|BinaryWorker|BinaryMigrate)
# calls ValidateForAPI / ValidateForWorker / ValidateForMigrate, and a binary
# whose contract is not satisfied EXITS 1 AT STARTUP.
#
# That is not a theoretical risk on this stack. On 2026-08-04 a fail-fast check
# added to the shared validator made cmd/migrate exit 1; backend and worker sit
# behind `depends_on: migrate: condition: service_completed_successfully`, so
# they never started and the site served 404s while Postgres stayed healthy.
# #76 is the fix for that coupling — but it also adds NEW checks (TTL ranges,
# https BASE_URL, COOKIE_SECURE, JWT_ISSUER, the seven trigger URLs, the SES
# block, DEV_EMAIL_OVERRIDE) that a long-lived .env has never been held to.
#
# Run this BEFORE deploying #76. It reads an env file and re-implements every
# startup-blocking condition, per binary.
#
#   ./preflight-phase1.sh                          # infra/.env.production
#   ./preflight-phase1.sh --env-file /opt/openmentor/infra/.env    # on the VM
#   ./preflight-phase1.sh --binary api             # one contract only
#
# NO VALUE IS EVER PRINTED. Every line reports a KEY NAME and a verdict, the
# same discipline as check-service-env.sh — this file is meant to be run against
# production secrets and its output pasted into a ticket.
#
# Exit codes:
#   0  every startup-blocking condition passes
#   1  at least one FAIL — deploying #76 takes that binary down
#   2  no FAIL, but at least one WARN (not startup-blocking; read them)
#   3  usage error / env file unreadable
#
# Emulation notes (kept honest deliberately):
#   - viper's AutomaticEnv runs with allowEmptyEnv=false, so `KEY=` is treated
#     as UNSET and the SetDefault value wins. This script does the same.
#   - GetInt is cast.ToInt: a non-numeric string becomes 0 with no error, which
#     is why `SESSION_TTL_HOURS=24h` is a startup failure and not a 24h TTL.
#   - GetBool is cast.ToBool: anything ParseBool cannot read becomes FALSE, so
#     COOKIE_SECURE=yes reads as false and fails the production check.
#   - The URL rules mirror api/pkg/safeurl: scheme allowlist, non-empty host,
#     no userinfo, and none of "'<>`\ TAB CR LF anywhere in the value.
# ============================================================================
set -uo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "$0")" && pwd)
ENV_FILE="$SCRIPT_DIR/.env.production"
WANT_BINARY=all

TAB=$(printf '\t')
CR=$(printf '\r')
# A literal backslash, built via tr's octal escape rather than written, so the
# case pattern below needs no escape shellcheck reads as a quoting mistake.
BACKSLASH=$(printf 'x' | tr 'x' '\134')

PASS_COUNT=0
FAIL_COUNT=0
WARN_COUNT=0

usage() {
    cat <<'EOF'
Usage: preflight-phase1.sh [--env-file PATH] [--binary api|worker|migrate|all]

  --env-file PATH   env file to check (default: infra/.env.production)
  --binary NAME     check one binary's contract only (default: all)
  -h, --help        this help

Reports PASS/FAIL/WARN per key. Never prints a value.
EOF
}

while [ $# -gt 0 ]; do
    case "$1" in
        --env-file)
            [ $# -ge 2 ] || { echo "❌ --env-file needs a path" >&2; exit 3; }
            ENV_FILE=$2
            shift 2
            ;;
        --binary)
            [ $# -ge 2 ] || { echo "❌ --binary needs a name" >&2; exit 3; }
            WANT_BINARY=$2
            shift 2
            ;;
        -h|--help) usage; exit 0 ;;
        *) echo "❌ unknown argument '$1'" >&2; usage >&2; exit 3 ;;
    esac
done

case "$WANT_BINARY" in
    api|worker|migrate|all) ;;
    *) echo "❌ --binary must be one of: api, worker, migrate, all" >&2; exit 3 ;;
esac

if [ ! -r "$ENV_FILE" ]; then
    echo "❌ cannot read env file: $ENV_FILE" >&2
    echo "   (on the VM the live file is /opt/openmentor/infra/.env)" >&2
    exit 3
fi

# ---------------------------------------------------------------------------
# Reading the env file
#
# The file is NEVER sourced: a production .env is not trusted input for this
# script, and `.` would execute anything in it. Values are extracted by name.
# ---------------------------------------------------------------------------

# env_raw KEY — last assignment wins, matching how a later line overrides an
# earlier one for docker compose.
env_raw() {
    sed -n "s/^[[:space:]]*$1=//p" "$ENV_FILE" | tail -n 1
}

# env_get KEY — the raw value with one layer of surrounding quotes removed.
env_get() {
    local v
    v=$(env_raw "$1")
    v=${v%"$CR"}
    case "$v" in
        '"'*'"') v=${v#\"}; v=${v%\"} ;;
        "'"*"'") v=${v#\'}; v=${v%\'} ;;
    esac
    printf '%s' "$v"
}

# trim VALUE — the Go side calls strings.TrimSpace before most emptiness tests.
trim() {
    local v=$1
    while :; do
        case "$v" in
            " "*|"$TAB"*) v=${v#?} ;;
            *) break ;;
        esac
    done
    while :; do
        case "$v" in
            *" "|*"$TAB") v=${v%?} ;;
            *) break ;;
        esac
    done
    printf '%s' "$v"
}

# effective KEY DEFAULT — an unset OR EMPTY key falls back to the viper default.
effective() {
    local v
    v=$(env_get "$1")
    if [ -z "$v" ]; then
        printf '%s' "$2"
    else
        printf '%s' "$v"
    fi
}

# ---------------------------------------------------------------------------
# Reporting — key names only, never values
# ---------------------------------------------------------------------------
if [ -t 1 ]; then
    C_PASS=$(printf '\033[32m'); C_FAIL=$(printf '\033[31m')
    C_WARN=$(printf '\033[33m'); C_DIM=$(printf '\033[2m'); C_OFF=$(printf '\033[0m')
else
    C_PASS=''; C_FAIL=''; C_WARN=''; C_DIM=''; C_OFF=''
fi

pass() {
    PASS_COUNT=$((PASS_COUNT + 1))
    printf '  %sPASS%s  %-38s %s%s%s\n' "$C_PASS" "$C_OFF" "$1" "$C_DIM" "${2:-}" "$C_OFF"
}
fail() {
    FAIL_COUNT=$((FAIL_COUNT + 1))
    printf '  %sFAIL%s  %-38s %s\n' "$C_FAIL" "$C_OFF" "$1" "$2"
}
warn() {
    WARN_COUNT=$((WARN_COUNT + 1))
    printf '  %sWARN%s  %-38s %s\n' "$C_WARN" "$C_OFF" "$1" "$2"
}
section() { printf '\n%s\n' "$1"; }

# ---------------------------------------------------------------------------
# Predicates that mirror the Go side
# ---------------------------------------------------------------------------

# is_int VALUE — what cast.ToInt can read without silently returning 0.
is_int() {
    local v=$1
    case "$v" in -*|+*) v=${v#?} ;; esac
    case "$v" in
        ''|*[!0-9]*) return 1 ;;
    esac
    return 0
}

# bool_true VALUE — strconv.ParseBool's true set, which is all cast.ToBool honours.
bool_true() {
    case "$1" in
        1|t|T|TRUE|true|True) return 0 ;;
    esac
    return 1
}

# bool_parseable VALUE — false here means cast.ToBool silently yields false.
bool_parseable() {
    case "$1" in
        1|t|T|TRUE|true|True|0|f|F|FALSE|false|False) return 0 ;;
    esac
    return 1
}

# has_unsafe_chars VALUE — safeurl's unsafeChars = "\"'<>`\\ \t\r\n"
has_unsafe_chars() {
    case "$1" in
        *'"'*|*"'"*|*'<'*|*'>'*|*'`'*|*"$BACKSLASH"*|*' '*|*"$TAB"*|*"$CR"*) return 0 ;;
    esac
    return 1
}

# url_ok VALUE MODE — MODE is "https" or "http-or-https". Mirrors safeurl.parse:
# scheme allowlist, non-empty host, no userinfo, no unsafe characters.
url_ok() {
    local raw=$1 mode=$2 rest host
    has_unsafe_chars "$raw" && return 1
    case "$raw" in
        https://*) rest=${raw#https://} ;;
        http://*)
            [ "$mode" = "http-or-https" ] || return 1
            rest=${raw#http://}
            ;;
        *) return 1 ;;
    esac
    host=${rest%%/*}
    host=${host%%\?*}
    host=${host%%#*}
    [ -n "$host" ] || return 1
    case "$host" in *@*) return 1 ;; esac
    return 0
}

# ---------------------------------------------------------------------------
# Environment-wide facts every contract branches on
# ---------------------------------------------------------------------------
APP_ENV=$(effective APP_ENV production)     # SetDefault("APP_ENV", "production")
IS_PROD=no
[ "$APP_ENV" = "production" ] && IS_PROD=yes

printf '%s\n' "Phase 1 preflight — per-binary configuration contract (PR #76 / H14)"
printf '%s\n' "env file : $ENV_FILE"
printf '%s\n' "APP_ENV  : $APP_ENV  (IsProduction=$IS_PROD — this gates 9 of the checks)"
printf '%s\n' "binary   : $WANT_BINARY"
printf '%s\n' "no value from the env file is printed below, only key names"

# ---------------------------------------------------------------------------
# Reusable check bodies (shared between the api and worker contracts)
# ---------------------------------------------------------------------------

check_database_url() {
    local dsn offline
    dsn=$(env_get DATABASE_URL)
    offline=$(env_get DB_WORK_OFFLINE)
    if [ -n "$dsn" ]; then
        pass DATABASE_URL "set"
    elif bool_true "$offline"; then
        warn DATABASE_URL "empty, but DB_WORK_OFFLINE is true so validation passes — the app has no database"
    else
        fail DATABASE_URL "empty and DB_WORK_OFFLINE is not true: 'DATABASE_URL is required when not in offline mode'"
    fi
}

check_analytics() {
    local provider posthog_enabled key host capture
    provider=$(trim "$(env_get ANALYTICS_PROVIDER)")
    posthog_enabled=$(env_get POSTHOG_ENABLED)
    provider=$(printf '%s' "$provider" | tr '[:upper:]' '[:lower:]')
    if [ -z "$provider" ]; then
        if bool_true "$posthog_enabled"; then provider=posthog; else provider=none; fi
    fi
    case "$provider" in
        none|posthog) pass ANALYTICS_PROVIDER "resolves to '$provider'" ;;
        *) fail ANALYTICS_PROVIDER "resolves to an unsupported provider: 'ANALYTICS_PROVIDER must be one of: none, posthog'"; return ;;
    esac
    [ "$provider" = posthog ] || return 0

    key=$(trim "$(env_get POSTHOG_API_KEY)")
    if [ -n "$key" ]; then
        pass POSTHOG_API_KEY "set (required while ANALYTICS_PROVIDER=posthog)"
    else
        fail POSTHOG_API_KEY "'POSTHOG_API_KEY is required when ANALYTICS_PROVIDER=posthog'"
    fi
    host=$(trim "$(effective POSTHOG_HOST https://eu.i.posthog.com)")
    capture=$(trim "$(env_get POSTHOG_CAPTURE_ENDPOINT)")
    if [ -n "$host" ] || [ -n "$capture" ]; then
        pass POSTHOG_HOST "host or capture endpoint present"
    else
        fail POSTHOG_HOST "'POSTHOG_HOST or POSTHOG_CAPTURE_ENDPOINT is required when ANALYTICS_PROVIDER=posthog'"
    fi
}

check_profiling() {
    local enabled endpoint
    enabled=$(env_get O11Y_PROFILING_ENABLED)
    endpoint=$(env_get O11Y_PROFILING_ENDPOINT)
    if bool_true "$enabled" && [ -z "$endpoint" ]; then
        fail O11Y_PROFILING_ENDPOINT "'O11Y_PROFILING_ENDPOINT is required when profiling is enabled'"
    else
        pass O11Y_PROFILING_ENDPOINT "not required (profiling off, or endpoint set)"
    fi
}

check_worker_auth_token() {
    local token
    token=$(env_get WORKER_AUTH_TOKEN)
    if [ "$IS_PROD" = yes ] && [ -z "$token" ]; then
        fail WORKER_AUTH_TOKEN "'WORKER_AUTH_TOKEN is required in production'"
    else
        pass WORKER_AUTH_TOKEN "present, or not production"
    fi
}

# check_base_url LABEL — the api and worker messages differ, the rule does not.
check_base_url() {
    local url
    url=$(effective BASE_URL https://openmentor.io)
    if [ -z "$url" ]; then
        fail BASE_URL "empty: '$1'"
        return
    fi
    if [ "$IS_PROD" = yes ] && ! url_ok "$url" https; then
        fail BASE_URL "not an absolute https URL with a host, no userinfo, no \"'<>\` backslash/space: 'BASE_URL must be an absolute https URL with a host in production'"
    else
        pass BASE_URL "https with a host"
    fi
}

# ---------------------------------------------------------------------------
# cmd/api — ValidateForAPI
# ---------------------------------------------------------------------------
check_api() {
    section "cmd/api  (ValidateForAPI) — a FAIL here means the public API does not start"

    check_database_url
    check_analytics

    local v
    for v in INTERNAL_MENTORS_API MENTORS_API_LIST_AUTH_TOKEN TURNSTILE_SECRET_KEY; do
        if [ -n "$(env_get "$v")" ]; then
            pass "$v" "set"
        else
            fail "$v" "'$v is required' (all environments, not just production)"
        fi
    done

    v=$(env_get TURNSTILE_EXPECTED_HOSTNAME)
    if [ -z "$v" ]; then
        pass TURNSTILE_EXPECTED_HOSTNAME "unset (feature off)"
    else
        case "$v" in
            *:*|*/*|*' '*) fail TURNSTILE_EXPECTED_HOSTNAME "contains ':' '/' or a space: 'TURNSTILE_EXPECTED_HOSTNAME must be a bare hostname, not a URL'" ;;
            *) pass TURNSTILE_EXPECTED_HOSTNAME "bare hostname" ;;
        esac
    fi

    # Not startup-blocking, and worse than one: the forms render the Turnstile
    # widget with no `action`, so siteverify echoes an empty action back and any
    # non-empty expectation rejects EVERY public write (D63). It ships inert.
    v=$(trim "$(env_get TURNSTILE_EXPECTED_ACTION)")
    if [ -z "$v" ]; then
        pass TURNSTILE_EXPECTED_ACTION "unset (this knob is inert by design — keep it unset)"
    else
        warn TURNSTILE_EXPECTED_ACTION "NOT startup-blocking, but any non-empty value rejects EVERY public write (contact form, reviews, registration) — see DECISIONS D63"
    fi

    if [ -n "$(effective PORT 8081)" ]; then
        pass PORT "set (default 8081)"
    else
        fail PORT "'PORT is required'"
    fi

    check_base_url "BASE_URL is required"

    # ALLOWED_CORS_ORIGINS: at least one non-blank element, and no bare '*'
    # (credentials are enabled on the CORS middleware).
    local origins origin found_any found_star oldifs
    origins=$(effective ALLOWED_CORS_ORIGINS "https://openmentor.io,https://www.openmentor.io")
    found_any=no
    found_star=no
    oldifs=$IFS
    IFS=','
    for origin in $origins; do
        origin=$(trim "$origin")
        [ -n "$origin" ] && found_any=yes
        [ "$origin" = '*' ] && found_star=yes
    done
    IFS=$oldifs
    if [ "$found_any" != yes ]; then
        fail ALLOWED_CORS_ORIGINS "no non-blank origin: 'ALLOWED_CORS_ORIGINS is required'"
    elif [ "$found_star" = yes ]; then
        fail ALLOWED_CORS_ORIGINS "'ALLOWED_CORS_ORIGINS must not contain '\''*'\'' (credentials are enabled)'"
    else
        pass ALLOWED_CORS_ORIGINS "at least one origin, no wildcard"
    fi

    # --- session contract ---------------------------------------------------
    local secret
    secret=$(env_get JWT_SECRET)
    if [ -z "$secret" ]; then
        if [ "$IS_PROD" = yes ]; then
            fail JWT_SECRET "'JWT_SECRET is required in production'"
        else
            pass JWT_SECRET "empty, allowed off production"
        fi
    elif [ "${#secret}" -lt 32 ]; then
        fail JWT_SECRET "shorter than 32 bytes: 'JWT_SECRET must be at least 32 characters' (length not printed)"
    else
        pass JWT_SECRET "set, at least 32 bytes"
    fi

    v=$(effective SESSION_TTL_HOURS 24)
    if ! is_int "$v"; then
        fail SESSION_TTL_HOURS "not an integer — viper coerces it to 0 and the range check then fails: 'SESSION_TTL_HOURS must be between 1 and 720, got 0' (a value like 24h is the classic case)"
    elif [ "$v" -lt 1 ] || [ "$v" -gt 720 ]; then
        fail SESSION_TTL_HOURS "outside 1..720 (720 = 30 days): 'SESSION_TTL_HOURS must be between 1 and 720'"
    else
        pass SESSION_TTL_HOURS "integer within 1..720"
    fi

    v=$(effective LOGIN_TOKEN_TTL_MINUTES 15)
    if ! is_int "$v"; then
        fail LOGIN_TOKEN_TTL_MINUTES "not an integer — viper coerces it to 0: 'LOGIN_TOKEN_TTL_MINUTES must be between 1 and 120, got 0' (a value like 15m is the classic case)"
    elif [ "$v" -lt 1 ] || [ "$v" -gt 120 ]; then
        fail LOGIN_TOKEN_TTL_MINUTES "outside 1..120: 'LOGIN_TOKEN_TTL_MINUTES must be between 1 and 120'"
    else
        pass LOGIN_TOKEN_TTL_MINUTES "integer within 1..120"
    fi

    v=$(effective JWT_ISSUER openmentor-api)
    if [ -z "$v" ]; then
        fail JWT_ISSUER "'JWT_ISSUER is required (it is checked when a session is verified)'"
    elif [ "$v" = "openmentor-api" ]; then
        pass JWT_ISSUER "the default, which is what every live token was minted with"
    else
        # PR #75 starts VALIDATING iss (jwt.WithIssuer). A value that differs
        # from the one live tokens carry rejects every existing cookie.
        warn JWT_ISSUER "set to a non-default value — PR #75 starts validating 'iss', so if this differs from the issuer live tokens were minted with, EVERY mentor and moderator is logged out. Confirm it has not changed since the last deploy."
    fi

    v=$(env_get COOKIE_SECURE)
    if [ -z "$v" ]; then
        pass COOKIE_SECURE "unset, defaults to true"
    elif bool_true "$v"; then
        pass COOKIE_SECURE "true"
    elif bool_parseable "$v"; then
        if [ "$IS_PROD" = yes ]; then
            fail COOKIE_SECURE "explicitly false: 'COOKIE_SECURE must not be disabled in production'"
        else
            pass COOKIE_SECURE "false, allowed off production"
        fi
    else
        if [ "$IS_PROD" = yes ]; then
            fail COOKIE_SECURE "not a boolean literal — cast.ToBool silently reads it as FALSE, so this fails: 'COOKIE_SECURE must not be disabled in production'. Use true/1/t/T/TRUE/True."
        else
            warn COOKIE_SECURE "not a boolean literal — silently reads as false"
        fi
    fi

    check_worker_auth_token

    # --- the seven required event triggers, plus format and the derivation ---
    local trig missing
    missing=''
    for trig in MENTOR_CREATED_TRIGGER_URL \
                MENTOR_REQUEST_CREATED_TRIGGER_URL \
                MENTOR_LOGIN_EMAIL_TRIGGER_URL \
                MODERATOR_LOGIN_EMAIL_TRIGGER_URL \
                MENTOR_MODERATION_TRIGGER_URL \
                REQUEST_PROCESS_FINISHED_TRIGGER_URL \
                REVIEW_CREATED_TRIGGER_URL; do
        v=$(trim "$(env_get "$trig")")
        if [ -z "$v" ]; then
            if [ "$IS_PROD" = yes ]; then
                fail "$trig" "empty — one of the seven required in production: 'event trigger URLs are required in production'"
                missing="$missing $trig"
            else
                pass "$trig" "empty, allowed off production"
            fi
        else
            pass "$trig" "set"
        fi
    done

    # Format applies to all EIGHT, in every environment. http is allowed here:
    # these are dialed as http://worker:8090/jobs/... inside the Docker network.
    for trig in MENTOR_CREATED_TRIGGER_URL \
                MENTOR_UPDATED_TRIGGER_URL \
                MENTOR_REQUEST_CREATED_TRIGGER_URL \
                MENTOR_LOGIN_EMAIL_TRIGGER_URL \
                MODERATOR_LOGIN_EMAIL_TRIGGER_URL \
                MENTOR_MODERATION_TRIGGER_URL \
                REQUEST_PROCESS_FINISHED_TRIGGER_URL \
                REVIEW_CREATED_TRIGGER_URL; do
        v=$(trim "$(env_get "$trig")")
        [ -n "$v" ] || continue
        if url_ok "$v" http-or-https; then
            pass "$trig (format)" "absolute http(s) URL with a host"
        else
            fail "$trig (format)" "'$trig must be an absolute http(s) URL with a host'"
        fi
    done

    # MENTOR_UPDATED_TRIGGER_URL is deliberately unset in production: the worker
    # serves no such job. Only its format is checked, above.
    if [ -z "$(trim "$(env_get MENTOR_UPDATED_TRIGGER_URL)")" ]; then
        pass MENTOR_UPDATED_TRIGGER_URL "unset — correct, the worker has no such endpoint"
    fi

    # The derivation guard. mentor-confirmed and mentor-confirm-email are built
    # from MENTOR_CREATED_TRIGGER_URL by substituting this one path segment; if
    # the segment is absent, derivation returns "" and both confirmation-email
    # triggers silently do nothing. #76 makes that a startup failure instead.
    v=$(env_get MENTOR_CREATED_TRIGGER_URL)
    if [ -z "$v" ]; then
        : # already reported as missing above
    else
        case "$v" in
            *new-mentor-watcher*)
                pass "MENTOR_CREATED_TRIGGER_URL (derivation)" \
                    "contains 'new-mentor-watcher' — mentor-confirmed + mentor-confirm-email derive cleanly"
                ;;
            *)
                fail "MENTOR_CREATED_TRIGGER_URL (derivation)" \
                    "does not contain 'new-mentor-watcher': the mentor-confirmed and mentor-confirm-email triggers are derived from it by substituting that path segment, so both confirmation emails would be silently disabled"
                ;;
        esac
    fi

    # --- object storage: complete or (off production) entirely empty ---------
    local s3 s3_missing s3_present
    s3_missing=0
    s3_present=0
    for s3 in S3_STORAGE_ACCESS_KEY S3_STORAGE_SECRET_KEY S3_STORAGE_BUCKET \
              S3_STORAGE_ENDPOINT S3_STORAGE_REGION; do
        if [ -z "$(trim "$(env_get "$s3")")" ]; then
            s3_missing=$((s3_missing + 1))
        else
            s3_present=$((s3_present + 1))
        fi
    done
    if [ "$s3_missing" -eq 0 ]; then
        pass "S3_STORAGE_* (5 keys)" "complete"
    elif [ "$s3_present" -gt 0 ]; then
        fail "S3_STORAGE_* (5 keys)" "$s3_missing of 5 blank: 'S3 object storage is partially configured' — a partial block is rejected in EVERY environment"
    elif [ "$IS_PROD" = yes ]; then
        fail "S3_STORAGE_* (5 keys)" "all 5 blank: 'S3 object storage is required in production'"
    else
        pass "S3_STORAGE_* (5 keys)" "all blank, allowed off production (uploads self-disable)"
    fi

    check_profiling
}

# ---------------------------------------------------------------------------
# cmd/worker — ValidateForWorker
# ---------------------------------------------------------------------------
check_worker() {
    section "cmd/worker  (ValidateForWorker) — a FAIL here means no email is ever sent"

    check_database_url
    check_analytics

    if [ -n "$(effective WORKER_PORT 8090)" ]; then
        pass WORKER_PORT "set (default 8090)"
    else
        fail WORKER_PORT "'WORKER_PORT is required'"
    fi

    check_worker_auth_token
    check_base_url "BASE_URL is required (it is rendered into every email link)"

    # SES: all three present, or (production aside) all three absent.
    local ses ses_missing ses_present
    ses_missing=0
    ses_present=0
    for ses in SES_REGION SES_ACCESS_KEY_ID SES_SECRET_ACCESS_KEY; do
        if [ -z "$(trim "$(env_get "$ses")")" ]; then
            ses_missing=$((ses_missing + 1))
        else
            ses_present=$((ses_present + 1))
        fi
    done
    if [ "$ses_missing" -eq 0 ]; then
        pass "SES_REGION/ACCESS_KEY_ID/SECRET" "complete"
    elif [ "$ses_present" -gt 0 ]; then
        fail "SES_REGION/ACCESS_KEY_ID/SECRET" "$ses_missing of 3 blank: 'SES email delivery is partially configured' — rejected in EVERY environment"
    elif [ "$IS_PROD" = yes ]; then
        fail "SES_REGION/ACCESS_KEY_ID/SECRET" "all 3 blank: 'SES email delivery is required in production'"
    else
        pass "SES_REGION/ACCESS_KEY_ID/SECRET" "all blank, allowed off production"
    fi

    if [ -n "$(trim "$(effective MODERATORS_EMAIL moderators@openmentor.io)")" ]; then
        pass MODERATORS_EMAIL "set (default moderators@openmentor.io)"
    else
        fail MODERATORS_EMAIL "'MODERATORS_EMAIL is required (moderation notifications have no other recipient)'"
    fi

    # The sneakiest new check: a staging value left in a production .env is now
    # fatal, where it previously just rerouted every mentor's magic link.
    local override
    override=$(trim "$(env_get DEV_EMAIL_OVERRIDE)")
    if [ -z "$override" ]; then
        pass DEV_EMAIL_OVERRIDE "empty — required in production"
    elif [ "$IS_PROD" = yes ]; then
        fail DEV_EMAIL_OVERRIDE "NON-EMPTY in production: 'DEV_EMAIL_OVERRIDE must be empty in production: it reroutes every recipient'. The worker will not start."
    else
        warn DEV_EMAIL_OVERRIDE "set — every recipient is rerouted to it (fine off production, fatal in it)"
    fi

    local discord
    discord=$(trim "$(env_get DISCORD_MENTORS_PRIVATE_INVITE_LINK)")
    if [ -z "$discord" ]; then
        pass DISCORD_MENTORS_PRIVATE_INVITE_LINK "unset (optional)"
    elif url_ok "$discord" https; then
        pass DISCORD_MENTORS_PRIVATE_INVITE_LINK "absolute https URL"
    else
        fail DISCORD_MENTORS_PRIVATE_INVITE_LINK "'DISCORD_MENTORS_PRIVATE_INVITE_LINK must be an absolute https URL with a host' — checked in EVERY environment, not just production"
    fi

    check_profiling
}

# ---------------------------------------------------------------------------
# cmd/migrate — ValidateForMigrate
# ---------------------------------------------------------------------------
check_migrate() {
    section "cmd/migrate  (ValidateForMigrate) — a FAIL here blocks the WHOLE stack"
    printf '  %sbackend and worker sit behind depends_on: service_completed_successfully,%s\n' "$C_DIM" "$C_OFF"
    printf '  %sso a migrate that exits 1 leaves both in Created and the site serving 404.%s\n' "$C_DIM" "$C_OFF"

    local dsn
    dsn=$(trim "$(env_get DATABASE_URL)")
    if [ -n "$dsn" ]; then
        pass DATABASE_URL "set — and it is now the ONLY thing migrate validates"
    else
        fail DATABASE_URL "'DATABASE_URL is required to run migrations' — note DB_WORK_OFFLINE is deliberately ignored here"
    fi

    # Everything else migrate used to demand is gone. Flagging leftovers is
    # useful because check-service-env.sh will report the allowlist drift.
    local leftover found
    found=''
    for leftover in JWT_SECRET WORKER_AUTH_TOKEN INTERNAL_MENTORS_API \
                    MENTORS_API_LIST_AUTH_TOKEN TURNSTILE_SECRET_KEY \
                    S3_STORAGE_ACCESS_KEY BASE_URL ALLOWED_CORS_ORIGINS; do
        [ -n "$(env_get "$leftover")" ] && found="yes"
    done
    if [ -n "$found" ]; then
        pass "migrate: validation-only keys" "still in the env file, which is harmless — #76 removes them from migrate's compose block and allowlist, so they simply stop being passed to the container"
    fi
}

# ---------------------------------------------------------------------------
# Run
# ---------------------------------------------------------------------------
case "$WANT_BINARY" in
    api)     check_api ;;
    worker)  check_worker ;;
    migrate) check_migrate ;;
    all)     check_api; check_worker; check_migrate ;;
esac

section "Summary"
printf '  %s pass, %s fail, %s warn\n' "$PASS_COUNT" "$FAIL_COUNT" "$WARN_COUNT"

if [ "$FAIL_COUNT" -gt 0 ]; then
    cat <<'EOF'

❌ At least one binary will REFUSE TO START on the PR #76 image.

   Fix the keys above in the env file this ran against and re-run. If the FAIL
   is on cmd/migrate, the whole stack stays down (backend and worker wait on
   `migrate: condition: service_completed_successfully`) — that is the
   2026-08-04 outage shape.

   Editing production: change infra/.env.production on the workstation and run
   `./deploy.sh infra`, which uploads it under the deploy lock. Editing
   /opt/openmentor/infra/.env by hand on the VM works but is overwritten by the
   next deploy.
EOF
    exit 1
fi

if [ "$WARN_COUNT" -gt 0 ]; then
    cat <<'EOF'

⚠️  No startup-blocking problem, but read the WARN lines. They cover the two
    failure modes that are worse than a refused start because they are silent:
    a non-empty TURNSTILE_EXPECTED_ACTION (every public write rejected) and a
    changed JWT_ISSUER (every session rejected).
EOF
    exit 2
fi

printf '\n✅ Every per-binary startup condition in PR #76 is satisfied by this env file.\n'
exit 0
