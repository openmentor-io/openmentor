#!/bin/bash
set -euo pipefail

# Postgres advisory locks live in ONE space per cluster, shared by every
# component that takes one. Two components that pick the same key exclude each
# other silently: no error, no log, just a job or a script that waits for — or
# is refused by — something entirely unrelated to it. Nothing in the type system
# or the SQL can catch that, because the keys are plain integers in four
# different files across two languages.
#
# So this asserts the whole inventory at once: which keys are deliberately
# SHARED, which must be distinct, and that the inventory is complete. Adding a
# fifth lock without adding it here is the failure mode this is aimed at, which
# is why the count is asserted too.
#
# C12/D82 added the cron lease and the migration-intents consumer lock, taking
# the inventory from two keys to four.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

fail() {
    echo -e "${RED}❌ $1${NC}" >&2
    exit 1
}

# extract <file> <regex-with-one-capture> — the numeric literal, or empty.
extract() {
    local file="$1" pattern="$2" value
    [ -f "$file" ] || fail "advisory-lock inventory names a file that does not exist: $file"
    value="$(sed -nE "s/.*${pattern}.*/\1/p" "$file" | head -1)"
    [ -n "$value" ] || fail "could not find the advisory-lock key in $file (pattern: $pattern)"
    printf '%s' "$value"
}

# Normalize to decimal so 0x736c7567 and 1936748391 compare equal.
to_decimal() {
    printf '%d' "$(( $1 ))"
}

echo "Checking Postgres advisory-lock namespace inventory..."

# 1. Slug reservation, Go side (api/internal/repository/slug_history_repository.go)
GO_SLUG="$(extract "$REPO_ROOT/api/internal/repository/slug_history_repository.go" \
    'const slugAdvisoryLockNamespace = (0x[0-9a-fA-F]+|[0-9]+)')"

# 2. Slug reservation, migration-script side — DELIBERATELY the same key, so the
#    direct writer serializes against the Go registration/rename paths.
JS_SLUG="$(extract "$REPO_ROOT/infra/migration/migrate-mentors.js" \
    'const SLUG_ADVISORY_LOCK_NS = (0x[0-9a-fA-F]+|[0-9]+)')"

# 3. Cron job lease (api/internal/worker/repository.go, D82)
CRON_LEASE="$(extract "$REPO_ROOT/api/internal/worker/repository.go" \
    'const jobLeaseNamespace = (0x[0-9a-fA-F]+|[0-9]+)')"

# 4. migration_intents consumer lock (infra/migration/migrate-mentors.js, C12)
INTENTS="$(extract "$REPO_ROOT/infra/migration/migrate-mentors.js" \
    'const INTENTS_ADVISORY_LOCK_NS = (0x[0-9a-fA-F]+|[0-9]+)')"

# 5. Test-schema bootstrap (api/test/dbtest/dbtest.go). Single-int form, but the
#    single-int and two-int spaces are separate in Postgres, so it cannot collide
#    with the four above by construction. Asserted anyway: it is the one key an
#    author is most likely to copy when adding a fifth.
DBTEST="$(extract "$REPO_ROOT/api/test/dbtest/dbtest.go" \
    'const schemaLockKey = (0x[0-9a-fA-F]+|[0-9]+)')"

GO_SLUG_D="$(to_decimal "$GO_SLUG")"
JS_SLUG_D="$(to_decimal "$JS_SLUG")"
CRON_LEASE_D="$(to_decimal "$CRON_LEASE")"
INTENTS_D="$(to_decimal "$INTENTS")"
DBTEST_D="$(to_decimal "$DBTEST")"

# --- The one pair that MUST match -----------------------------------------
if [ "$GO_SLUG_D" != "$JS_SLUG_D" ]; then
    fail "slug namespaces diverged: Go has $GO_SLUG ($GO_SLUG_D), migrate-mentors.js has $JS_SLUG ($JS_SLUG_D). They must be equal — that shared key is what serializes the migration script's direct slug writes against the API's."
fi
echo "  ✅ slug reservation shares one key across Go and the migration script ($GO_SLUG)"

# --- Every other pair must differ -----------------------------------------
# Named so a failure says which two collided, not just "duplicate found".
NAMES=("slug_reservation" "cron_job_lease" "migration_intents_consumer" "dbtest_schema_bootstrap")
VALUES=("$GO_SLUG_D" "$CRON_LEASE_D" "$INTENTS_D" "$DBTEST_D")

count="${#VALUES[@]}"
i=0
while [ "$i" -lt "$count" ]; do
    j=$((i + 1))
    while [ "$j" -lt "$count" ]; do
        if [ "${VALUES[$i]}" = "${VALUES[$j]}" ]; then
            fail "advisory-lock collision: ${NAMES[$i]} and ${NAMES[$j]} both use ${VALUES[$i]}. Two unrelated components would silently exclude each other."
        fi
        j=$((j + 1))
    done
    i=$((i + 1))
done
echo "  ✅ the $count distinct advisory-lock namespaces do not collide"

# --- The inventory is complete --------------------------------------------
# Every pg_advisory / pg_try_advisory call site in the repo has to be one of the
# keys above. A new one shows up here as an unaccounted-for file.
EXPECTED=(
    "api/internal/repository/slug_history_repository.go"
    "api/internal/worker/repository.go"
    "api/test/dbtest/dbtest.go"
    "infra/migration/migrate-mentors.js"
)

# No `mapfile`/`readarray`: these scripts have to run under the bash 3.2 that
# ships on macOS, where neither exists.
site_count=0
while IFS= read -r site; do
    [ -n "$site" ] || continue
    site_count=$((site_count + 1))
    found=false
    for expected in "${EXPECTED[@]}"; do
        if [ "$site" = "$expected" ]; then
            found=true
            break
        fi
    done
    if [ "$found" = false ]; then
        fail "$site takes a Postgres advisory lock but is not in this script's inventory. Add its key here so a collision cannot be introduced silently."
    fi
done <<EOF
$(cd "$REPO_ROOT" && git grep -l -E 'pg_(try_)?advisory(_xact)?_lock' -- '*.go' 'infra/migration/*.js' | sort)
EOF

if [ "$site_count" -ne "${#EXPECTED[@]}" ]; then
    fail "found $site_count advisory-lock call sites but the inventory lists ${#EXPECTED[@]}. A lock was removed without updating this script."
fi
echo "  ✅ every advisory-lock call site is accounted for ($site_count files)"

echo -e "${GREEN}✅ Advisory-lock namespace checks passed!${NC}"
