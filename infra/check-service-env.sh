#!/usr/bin/env bash
# ============================================================================
# Per-service environment allowlist check (SECURITY P10)
# ============================================================================
# Renders docker-compose.yml to JSON and asserts, per service:
#
#   1. every environment KEY is listed in that service's section of
#      env-allowlist.txt  (adding a key to compose without the allowlist fails)
#   2. the sensitive keys in SECRET_OWNERS below appear ONLY in the services
#      allowed to hold them  (so widening the allowlist file alone cannot
#      quietly hand DATABASE_URL to the internet-facing frontend)
#
# The compose file is parsed for the keys it DECLARES first, and every bare
# `- KEY` entry is seeded before rendering: compose drops a value-less entry it
# cannot resolve, which would hide a key that exists only in the production
# environment from both checks above. See declared_keys().
#
# Only KEY NAMES are ever read or printed. Values are never touched, so this is
# safe to run in CI on a production-shaped .env.
#
# Usage:
#   ./check-service-env.sh              # check
#   ./check-service-env.sh --self-test  # prove the check actually fails
#
# Runs in CI via `make check` (the "Infra: checks" step of
# .github/workflows/checks.yml, the required gate) on any PR touching infra/.
# Locally: `cd infra && make check`.
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")"

ALLOWLIST=env-allowlist.txt
COMPOSE_FILE=docker-compose.yml

# key -> space-separated services permitted to receive it. Anything not listed
# here is governed by the allowlist file alone; these are the ones where a
# mistake is a security incident rather than a config bug.
SECRET_OWNERS="
DATABASE_URL=backend worker migrate
POSTGRES_USER=postgres postgres-backup
POSTGRES_PASSWORD=postgres postgres-backup
POSTGRES_DB=postgres postgres-backup
POSTGRES_OBS_DSN=alloy
JWT_SECRET=backend worker migrate
WORKER_AUTH_TOKEN=backend worker migrate
INTERNAL_MENTORS_API=backend worker migrate
MENTORS_API_LIST_AUTH_TOKEN=backend worker migrate
TURNSTILE_SECRET_KEY=backend worker migrate
GO_API_INTERNAL_TOKEN=frontend
METRICS_AUTH_TOKEN=frontend alloy
S3_STORAGE_ACCESS_KEY=backend worker migrate
S3_STORAGE_SECRET_KEY=backend worker migrate
SES_ACCESS_KEY_ID=worker
SES_SECRET_ACCESS_KEY=worker
BACKUP_AWS_ACCESS_KEY_ID=postgres-backup
BACKUP_AWS_SECRET_ACCESS_KEY=postgres-backup
CLOUDFLARE_DNS_API_TOKEN=traefik
GCLOUD_RW_API_KEY=alloy
POSTHOG_API_KEY=backend worker migrate
POSTHOG_PERSONAL_API_KEY=
POSTHOG_PROJECT_ID=
FARO_API_KEY=
AWS_REGION=
ECR_REGISTRY=
VM_SSH_HOST=
VM_SSH_USER=
VM_SSH_KEY_FILE=
LETSENCRYPT_EMAIL=
IMAGE_TAG=
FRONTEND_IMAGE_TAG=
BACKEND_IMAGE_TAG=
"

command -v jq >/dev/null || { echo "error: jq is required" >&2; exit 1; }
command -v docker >/dev/null || { echo "error: docker is required" >&2; exit 1; }

# Compose needs an env file to interpolate. Use the real .env when present,
# otherwise a throwaway copy of the template - the check only reads key names,
# so template placeholders are fine.
TMPDIR_CHECK=$(mktemp -d)
trap 'rm -rf "$TMPDIR_CHECK"' EXIT
if [ -f .env ]; then
    SOURCE_ENV=.env
else
    SOURCE_ENV=.env.example
fi

# Every environment entry the given compose files DECLARE, as
# "<service> <KEY> bare|valued". Read from the YAML on purpose: a bare `- KEY`
# entry (inherit from the deploy environment) is DROPPED from the rendered
# service when the key has no value at render time - compose v2 removes it,
# v5 renders it null. CI interpolates from .env.example, so a `- NEW_SECRET`
# that only exists in the production .env would vanish before check() sees it
# and BOTH layers would pass a service that really does receive it.
# Unrecognised entries are a hard error rather than a silent omission.
declared_keys() {
    awk '
        function ind(s) { match(s, /^ */); return RLENGTH }
        /^services:[[:space:]]*$/                 { in_svcs = 1; in_env = 0; next }
        /^[^[:space:]#]/                          { in_svcs = 0; in_env = 0; next }
        !in_svcs                                  { next }
        /^[[:space:]]*#/                          { next }
        /^[[:space:]]*$/                          { next }
        in_env && ind($0) <= env_ind              { in_env = 0 }
        /^  [A-Za-z0-9_.-]+:[[:space:]]*$/ {
            svc = substr($0, 3); sub(/:.*/, "", svc); in_env = 0; next
        }
        !in_env && /^[[:space:]]+environment:[[:space:]]*$/ {
            in_env = 1; env_ind = ind($0); next
        }
        !in_env                                   { next }
        /^[[:space:]]*-[[:space:]]+[A-Za-z_][A-Za-z0-9_]*([[:space:]]*|[[:space:]]+#.*)$/ {
            print svc, $2, "bare"; next
        }
        /^[[:space:]]*-[[:space:]]+[A-Za-z_][A-Za-z0-9_]*=/ {
            key = $2; sub(/=.*/, "", key); print svc, key, "valued"; next
        }
        {
            printf "%s:%d: unrecognised environment entry: %s\n", FILENAME, FNR, $0 > "/dev/stderr"
            bad = 1
        }
        END { if (bad) exit 1 }
    ' "$@" | sort -u
}

# Render with every declared bare key seeded, so no entry can render away.
# Seeds are appended, never substituted: keys the env file already defines keep
# their real value, and the placeholder is not a credential.
seed_env_file() {
    local declared="$1" out="$2" key
    cp "$SOURCE_ENV" "$out"
    # $SOURCE_ENV may be the real .env; this copy is short-lived but is still a
    # full set of production values on disk.
    chmod 600 "$out"
    while read -r key; do
        grep -q "^${key}=" "$out" || printf '%s=seeded-for-key-check\n' "$key" >> "$out"
    done < <(awk '$3 == "bare" { print $2 }' "$declared" | sort -u)
}

# render_keys <env-file> [extra compose args] - render, then immediately reduce
# to key names only: the intermediate JSON holds real values, so it never
# leaves this pipeline.
render_keys() {
    local env_file="$1"; shift
    docker compose --env-file "$env_file" "$@" -f "$COMPOSE_FILE" config --format json 2>/dev/null |
        jq -r '.services | to_entries[] | .key as $s | (.value.environment // {} | keys[]?) | "\($s) \(.)"'
}

# Nothing the compose file declares may be missing from the render: that is the
# hole the seeding closes, so assert it instead of trusting it.
assert_nothing_dropped() {
    local declared="$1" rendered="$2" missing
    missing=$(comm -23 <(cut -d' ' -f1,2 "$declared" | sort -u) <(sort -u "$rendered"))
    [ -n "$missing" ] || return 0
    echo "error: compose dropped environment entries that $COMPOSE_FILE declares," >&2
    echo "       so they would escape the allowlist and secret-owner checks:" >&2
    printf '  %s\n' "$missing" >&2
    exit 1
}

section_keys() {
    awk -v svc="$1" '
        /^\[/ { in_section = ($0 == "[" svc "]"); next }
        in_section && /^[A-Z]/ { print $1 }
    ' "$ALLOWLIST"
}

check() {
    local keys_file="$1" failures=0 svc key allowed owners
    local services
    services=$(cut -d' ' -f1 "$keys_file" | sort -u)

    for svc in $services; do
        allowed=$(section_keys "$svc")
        if ! grep -q "^\[$svc\]$" "$ALLOWLIST"; then
            echo "FAIL $svc: no [$svc] section in $ALLOWLIST"
            failures=$((failures + 1))
            continue
        fi
        while read -r key; do
            if ! printf '%s\n' "$allowed" | grep -qx -- "$key"; then
                echo "FAIL $svc: '$key' is in $COMPOSE_FILE but not in [$svc] of $ALLOWLIST"
                failures=$((failures + 1))
            fi
        done < <(awk -v s="$svc" '$1 == s { print $2 }' "$keys_file")
    done

    # Sensitive keys: only their designated owners may receive them
    local owner_list
    while IFS= read -r line; do
        [ -n "$line" ] || continue
        key="${line%%=*}"
        owner_list="${line#*=}"
        owners=" $owner_list "
        while read -r svc; do
            case "$owners" in
                *" $svc "*) ;;
                *)
                    echo "FAIL $svc: '$key' must not reach this service (owners: ${owner_list:-none})"
                    failures=$((failures + 1))
                    ;;
            esac
        done < <(awk -v k="$key" '$2 == k { print $1 }' "$keys_file")
    done <<< "$SECRET_OWNERS"

    return "$failures"
}

DECLARED="$TMPDIR_CHECK/declared"
declared_keys "$COMPOSE_FILE" > "$DECLARED" || {
    echo "error: cannot enumerate the environment entries in $COMPOSE_FILE (see above)." >&2
    echo "       The seeding below depends on it, so refusing to report a pass." >&2
    exit 1
}
RENDER_ENV="$TMPDIR_CHECK/env"
seed_env_file "$DECLARED" "$RENDER_ENV"

KEYS="$TMPDIR_CHECK/keys"
render_keys "$RENDER_ENV" > "$KEYS"
assert_nothing_dropped "$DECLARED" "$KEYS"

if [ "${1:-}" = "--self-test" ]; then
    # Prove both layers fire, and that neither can be evaded by declaring the
    # key WITHOUT a value - the form the production-only secret would take.
    # Layer 1: a key absent from the allowlist. Layer 2: a key present in the
    # allowlist but owned by another service.
    OVERRIDE="$TMPDIR_CHECK/override.yml"
    cat > "$OVERRIDE" <<'YAML'
services:
  frontend:
    environment:
      - TOTALLY_NEW_KEY=x
      - DATABASE_URL=x
      - BARE_KEY_ONLY_SET_IN_PRODUCTION
YAML
    BAD_DECLARED="$TMPDIR_CHECK/bad-declared"
    declared_keys "$COMPOSE_FILE" "$OVERRIDE" > "$BAD_DECLARED"
    grep -qx 'frontend BARE_KEY_ONLY_SET_IN_PRODUCTION bare' "$BAD_DECLARED" || {
        echo "SELF-TEST FAILED: declared_keys did not see the bare entry, so it"
        echo "                  would never be seeded and could render away"
        exit 1; }
    BAD_ENV="$TMPDIR_CHECK/bad-env"
    seed_env_file "$BAD_DECLARED" "$BAD_ENV"
    grep -qx 'BARE_KEY_ONLY_SET_IN_PRODUCTION=seeded-for-key-check' "$BAD_ENV" || {
        echo "SELF-TEST FAILED: the bare key was not seeded into the render env"
        exit 1; }

    BAD="$TMPDIR_CHECK/bad-keys"
    render_keys "$BAD_ENV" -f "$OVERRIDE" > "$BAD"
    echo "--- self-test: injecting TOTALLY_NEW_KEY, DATABASE_URL and a bare"
    echo "    BARE_KEY_ONLY_SET_IN_PRODUCTION into frontend ---"
    if out=$(check "$BAD"); then
        echo "SELF-TEST FAILED: the check passed on a stack it should reject"
        exit 1
    fi
    echo "$out"
    echo "$out" | grep -q "frontend: 'TOTALLY_NEW_KEY' is in" || {
        echo "SELF-TEST FAILED: allowlist layer did not fire"; exit 1; }
    echo "$out" | grep -q "frontend: 'DATABASE_URL' must not reach" || {
        echo "SELF-TEST FAILED: secret-owner layer did not fire"; exit 1; }
    echo "$out" | grep -q "frontend: 'BARE_KEY_ONLY_SET_IN_PRODUCTION' is in" || {
        echo "SELF-TEST FAILED: a value-less entry rendered away, which is how a"
        echo "                  production-only secret slips past both layers"
        exit 1; }
    echo "--- self-test OK: both layers rejected the injected keys, valued and bare ---"
    echo
fi

echo "Rendered environment key count per service:"
while read -r svc; do
    printf '  %-18s %s\n' "$svc" "$(awk -v s="$svc" '$1 == s' "$KEYS" | wc -l | tr -d ' ')"
done < <(cut -d' ' -f1 "$KEYS" | sort -u)
echo

if check "$KEYS"; then
    echo "OK: every service's environment is within its allowlist, and no"
    echo "    sensitive key reaches a service that should not hold it."
else
    echo
    echo "Fix docker-compose.yml, or (if the key genuinely belongs there) add it"
    echo "to $ALLOWLIST and ENVIRONMENT_VARIABLES.md in the same commit."
    exit 1
fi
