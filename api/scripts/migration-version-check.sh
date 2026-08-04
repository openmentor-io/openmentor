#!/usr/bin/env bash
# ============================================================================
# Migration numbering guard — the collisions git merges cleanly
# ============================================================================
# Two branches that both add `000010_*.sql` are DIFFERENT files, so git merges
# them without a conflict and nothing complains. What that produces:
#
#   • duplicate versions — golang-migrate records one version number per applied
#     migration, so the second file with that number is never applied and never
#     reported as pending, and
#   • an out-of-order merge — `Up()` only walks FORWARD from the recorded
#     version, so a migration merged below a version production has already
#     applied is never applied THERE, while every fresh database (and every CI
#     run, and every developer) gets it. The divergence is invisible until a
#     query hits the missing column in production.
#
# So this fails when, in the merged tree:
#   1. two migrations share a version number,
#   2. a version is missing its .up.sql / .down.sql counterpart,
#   3. this branch ADDS a migration whose version is not higher than the highest
#      version on the base ref (origin/main) — i.e. it would merge in below
#      something production has already applied. Renumber at PR time instead.
#   4. the version sequence has a gap at or below the base ref's highest version
#      (a gap ABOVE it is a sibling PR that has not merged yet — see the block).
#
# Case 3 and the severity of case 4 need git; both degrade to a loud notice when
# the base ref is not in the clone (fetch it: git fetch --no-tags --depth=1
# origin main). Cases 1-2 are pure filesystem checks and always run.
#
# Missing .down.sql: tolerated for the versions in MIGRATION_DOWN_EXEMPT, the
# same allowance scripts/migration-test.sh makes. 000002_populate_tags has no
# down file on main; PR #74 adds it, after which this exemption should be
# deleted and the check covers the full range.
#
# Usage: ./scripts/migration-version-check.sh
#        make migration-check
# Env:   MIGRATIONS_DIR (default migrations), MIGRATION_BASE_REF (default
#        origin/main), MIGRATION_DOWN_EXEMPT (space-separated base names)
# ============================================================================
set -euo pipefail

cd "$(dirname "$0")/.."

MIGRATIONS_DIR=${MIGRATIONS_DIR:-migrations}
BASE_REF=${MIGRATION_BASE_REF:-origin/main}
DOWN_EXEMPT=${MIGRATION_DOWN_EXEMPT:-000002_populate_tags}

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

FAILURES=0
fail() { printf '❌ %s\n' "$1"; FAILURES=$((FAILURES + 1)); }
ok() { printf '  ok   %s\n' "$1"; }

# --- enumerate the migration files -----------------------------------------
# "<version> <name>" and "<name> <up|down>", one line each; <name> is the file
# name without the .up.sql/.down.sql suffix, so the two halves of one migration
# collapse to a single entry.
: > "$TMP/pairs"
: > "$TMP/kinds"
for f in "$MIGRATIONS_DIR"/*; do
    [ -f "$f" ] || continue
    b=${f##*/}
    case "$b" in
        *.up.sql)   base=${b%.up.sql};   kind=up ;;
        *.down.sql) base=${b%.down.sql}; kind=down ;;
        *)
            fail "$b: not a migration file — golang-migrate only reads <version>_<name>.up.sql / .down.sql and silently ignores everything else, so this would never run"
            continue
            ;;
    esac
    ver=${base%%_*}
    case "$ver" in
        '' | *[!0-9]*)
            fail "$b: does not start with a numeric version (<version>_<name>.<up|down>.sql)"
            continue
            ;;
    esac
    printf '%s %s\n' "$ver" "$base" >> "$TMP/pairs"
    printf '%s %s\n' "$base" "$kind" >> "$TMP/kinds"
done

sort -u "$TMP/pairs" > "$TMP/migrations"
if [ ! -s "$TMP/migrations" ]; then
    fail "no migrations found in $MIGRATIONS_DIR"
    echo
    echo "1 migration numbering check failed"
    exit 1
fi

# --- 1. one version, one migration -----------------------------------------
DUPES=$(awk '{print $1}' "$TMP/migrations" | sort | uniq -d)
if [ -n "$DUPES" ]; then
    for v in $DUPES; do
        names=$(awk -v v="$v" '$1 == v {printf "%s ", $2}' "$TMP/migrations")
        fail "version $v is claimed by more than one migration: ${names% } — golang-migrate stores one row per version, so only the first would ever be applied. Renumber all but one."
    done
else
    ok "every version number is claimed by exactly one migration"
fi

# --- 2. both halves present -------------------------------------------------
MISSING_DOWN=0
while read -r _ver base; do
    grep -qx "$base up" "$TMP/kinds" ||
        fail "$base has a .down.sql but no .up.sql — nothing would ever apply it"
    if ! grep -qx "$base down" "$TMP/kinds"; then
        case " $DOWN_EXEMPT " in
            *" $base "*)
                echo "  note $base has no .down.sql (known, exempted via MIGRATION_DOWN_EXEMPT; PR #74 adds it — same allowance scripts/migration-test.sh makes, which walks down only to that boundary)"
                MISSING_DOWN=1
                ;;
            *)
                fail "$base has no .down.sql — a rollback across it is impossible, and scripts/migration-test.sh can only walk down to the version above it"
                ;;
        esac
    fi
done < "$TMP/migrations"
if [ "$MISSING_DOWN" -eq 0 ]; then
    ok "every migration has both an .up.sql and a .down.sql"
fi

# Numeric, so a version written without its leading zeros still lands where it
# belongs.
VERSIONS=$(awk '{print $1 + 0}' "$TMP/migrations" | sort -n -u)

# "<numeric version> <name>" for every migration in a git ref
migrations_in_ref() {
    git ls-tree -r --name-only "$1" -- "$MIGRATIONS_DIR" 2>/dev/null \
        | sed -e 's|.*/||' -e 's|\.up\.sql$||' -e 's|\.down\.sql$||' \
        | awk -F_ 'NF > 1 {printf "%d %s\n", $1, $0}' \
        | sort -u
}

# --- 3. anything added here is newer than everything on the base ------------
BASE_MAX=""
if ! git rev-parse --verify --quiet "$BASE_REF^{commit}" >/dev/null 2>&1; then
    echo "  skip $BASE_REF is not in this clone, so 'added migrations must be newer than the base' was not checked."
    echo "       Fetch it first: git fetch --no-tags --depth=1 origin main"
else
    # Compared by MIGRATION, not by version number: `strict_required_status_
    # checks_policy` is off in the ruleset, so the tree CI tested can be older
    # than main. A PR adding 000010_b while main already merged 000010_a shows no
    # duplicate in that stale tree — but its migration is still one main does not
    # have, at a version main has already used.
    migrations_in_ref "$BASE_REF" > "$TMP/base-migrations"
    migrations_in_ref "$(git merge-base HEAD "$BASE_REF" 2>/dev/null || echo "$BASE_REF")" \
        | awk '{print $1}' | sort -n -u > "$TMP/mergebase-versions"
    if [ ! -s "$TMP/base-migrations" ]; then
        echo "  skip $BASE_REF carries no migrations — nothing to be newer than"
    else
        BASE_MAX=$(awk '{print $1}' "$TMP/base-migrations" | sort -n | tail -1)
        : > "$TMP/added"
        while read -r ver base; do
            grep -qx "$((10#$ver)) $base" "$TMP/base-migrations" ||
                printf '%s %s\n' "$((10#$ver))" "$base" >> "$TMP/added"
        done < "$TMP/migrations"
        if [ ! -s "$TMP/added" ]; then
            ok "no migration added on top of $BASE_REF (highest version there: $BASE_MAX)"
        else
            BAD=$(awk -v max="$BASE_MAX" '$1 <= max {printf "%s ", $2}' "$TMP/added")
            if [ -z "$BAD" ]; then
                ok "added migration(s) $(awk '{printf "%s ", $2}' "$TMP/added")are all above $BASE_REF's highest version ($BASE_MAX)"
            else
                fail "${BAD% } is added on this branch but is not above $BASE_REF's highest version ($BASE_MAX). golang-migrate's Up() only walks FORWARD from the recorded version, so a database already at $BASE_MAX would never apply it, while every fresh database does — a silent, production-only divergence. Renumber it above $BASE_MAX now. (If you instead RENAMED an existing migration, rename it back: the version stays applied under its old row either way.)"
            fi
        fi
    fi
fi

# --- 4. no gap in the sequence ---------------------------------------------
# A missing version number is a failure unless something benign explains it, and
# the two benign explanations are worth knowing, because failing them would make
# this check unusable:
#   • above the base ref's highest version -> a sibling PR holds it and has not
#     merged yet. Renumbering three concurrent PRs to 10/11/12 produces exactly
#     this, and calling it an error would turn the FIX for a collision into a new
#     one. It closes itself when the sibling merges.
#   • present on the base ref but absent from this branch's merge base -> main
#     added it after this branch forked and the tested tree is simply behind.
# Everything else means the number vanished on this branch (a delete or a
# rename), which is a real, silent hole in the sequence. Without a base ref to
# compare against, no gap can be explained and all of them fail.
PREV=""
GAP=0
for n in $VERSIONS; do
    if [ -n "$PREV" ] && [ "$n" -ne $((PREV + 1)) ]; then
        missing=$((PREV + 1))
        while [ "$missing" -lt "$n" ]; do
            if [ -n "$BASE_MAX" ] && [ "$missing" -gt "$BASE_MAX" ]; then
                echo "  note version $missing is missing, above $BASE_REF's highest ($BASE_MAX) — a sibling PR holds it; it closes when that merges"
            elif [ -n "$BASE_MAX" ] &&
                 awk -v v="$missing" '$1 == v {found = 1} END {exit !found}' "$TMP/base-migrations" &&
                 ! grep -qx "$missing" "$TMP/mergebase-versions"; then
                echo "  note version $missing is missing here but present on $BASE_REF — this branch predates it; the merge restores it"
            else
                fail "gap in the version sequence: version $missing is missing (between $PREV and $n). It was deleted or renumbered on this branch, so nothing will ever apply it."
                GAP=1
            fi
            missing=$((missing + 1))
        done
    fi
    PREV=$n
done
# A plain `[ ... ] && ok ...` here would end the script under `set -e` the moment
# a gap is found, skipping the summary below.
if [ "$GAP" -eq 0 ]; then
    ok "no unexplained gap in the version sequence ($(printf '%s' "$VERSIONS" | head -1)..$PREV)"
fi

echo
if [ "$FAILURES" -eq 0 ]; then
    echo "✅ migration numbering is sound"
else
    echo "$FAILURES migration numbering check(s) failed"
    exit 1
fi
