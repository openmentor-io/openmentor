# Runbooks: 2026-08 audit diagnostics & repair

Operator-facing material for the 2026-08 audit remediation. The audit found
defects that may **already** have damaged production data; these files are what
you run against production to find out, and what you do about what you find.

They accompany the remediation plan
([`../../audit/2026-08-remediation-plan.md`](../../audit/2026-08-remediation-plan.md))
— the plan is for whoever writes the code, this directory is for whoever holds
the production credentials. Item ids referenced below (`P2`, `P4`, `H4`, `P14`,
`P15`, …) are that plan's; [`../../audit/README.md`](../../audit/README.md)
indexes it and the source audits it was built from.

Everything here is read-only or explicitly guarded. Nothing here changes code.

## Run it in this order

1. **`diagnostics.sh`** — run the checks and write a report.
   ```bash
   ./diagnostics.sh --dry-run     # confirm preconditions, connect to nothing
   ./diagnostics.sh               # on the VM; psql inside openmentor-postgres
   ```
   Or, from a workstation with no copy of the script on the VM:
   `infra/db.sh < docs/runbooks/audit-2026-08/diagnostics.sql`.

   Read `--help` for `--local`, `--container`, `--out` and psql passthrough.
   Findings are not failures: it exits 0 either way.

2. **[`diagnostics.sql`](diagnostics.sql)** — the queries themselves, each with a
   comment block explaining what it detects, what a hit means, and what to do.
   Read this if you want to know what you are about to run, or to run a single
   check by hand.

3. **[`data-repair.md`](data-repair.md)** — what to do about D1 and D2 hits.
   **Read the section before running anything in it.** Both repairs have a way
   to make things worse, and both are flagged in caps.

4. **[`outstanding-decisions.md`](outstanding-decisions.md)** — the five things
   that need the owner. §1 and §2 are gated on the diagnostics above, so do them
   after step 1.

[`diagnostics_test.sh`](diagnostics_test.sh) is not part of the procedure — it is
the regression suite for the two files above, run by `Checks / required-checks`.
Run it before changing either of them:

```bash
./diagnostics_test.sh          # shell tier: no database needed
# full run, against a THROWAWAY database (it creates and drops om_diag_test):
docker run -d --name pg-diagtest -e POSTGRES_USER=openmentor \
    -e POSTGRES_PASSWORD=scratch -e POSTGRES_DB=openmentor \
    -p 55444:5432 postgres:16.14-alpine
PGHOST=127.0.0.1 PGPORT=55444 PGUSER=openmentor PGPASSWORD=scratch \
    PGDATABASE=openmentor OM_DIAG_TEST_DB=1 ./diagnostics_test.sh
docker rm -f pg-diagtest
```

## What the checks are

| Check | Detects | Repair |
|---|---|---|
| D1 / D1b | Mentors with `sort_order IS NULL` — they silently cannot log in, **and their public profile page cannot be rendered**. D1b separates lost registrations from imported profiles, because the repair for one damages the other. | `data-repair.md` §D1 |
| D2a–D2c | Prices overwritten with `Free` by an uncontrolled `<select>`, plus the exposure count. | `data-repair.md` §D2 |
| D2d | The same bug on the `experience` field. **Not in the audit plan** — found while writing these files. | `data-repair.md` §D2.1 |
| D3 | Whether the unescaped-email-template injection has been exercised in stored data. Covers all seven fields that reach an SES template: request description / name / **preferred contact**, mentor name / **price** / calendar URL, and mentor review. | investigation, not repair |
| D4 | Live review capabilities (completed requests with no review). | sizes the H4 redesign |

## Before you start

- **The report contains personal data** — names, email addresses, request text.
  `diagnostics.sh` creates it as a new file, mode 600, and **refuses a path that
  already exists** (an existing file keeps its own mode, so reusing one could
  publish the report). Keep it off shared drives and delete it when you are done
  (`../data-deletion.md` explains why that matters here).
- **The console only ever shows the SUMMARY block** — never detail rows, not even
  when a query fails. On failure you get psql's own error text and the report
  path; read the detail from the file.
- **`diagnostics.sql` pins the session read-only** (`default_transaction_read_only`,
  plus statement and lock timeouts). A stray write aborts instead of landing. No
  `ANALYZE`, no `EXPLAIN ANALYZE`, no exclusive locks.
- **The whole report is one snapshot.** D1–D4 and the summary run inside a single
  read-only `REPEATABLE READ` transaction, so the summary counts always match the
  detail rows above them even if someone saves a profile mid-run. It takes only
  `ACCESS SHARE` locks and blocks no writer.
- **Two repairs are gated on a code fix landing first.** D1's repair needs P2's
  `COALESCE`; D2's needs P4's form fix. Restoring prices before the form is fixed
  means the mentor's next save re-corrupts them.

## Still outstanding for the owner

Detail and implications in [`outstanding-decisions.md`](outstanding-decisions.md):

1. How many outstanding review invitations exist? (run D4 — it selects H4's
   migration shape)
2. Does a restorable pre-corruption `pg_dump` exist? (existence is not enough —
   confirm one restores)
3. Grafana Cloud / PostHog retention, and whether the plan supports **person**
   deletion for the `request:<uuid>` distinct_ids
4. Ko-fi account ownership and the correct URL (placeholder on every page)
5. One rehearsal of complete VM-loss recovery, plus an external uptime check

## What could not be verified from the repository

Stated so nobody mistakes a gap for an answer:

- **Production data.** Nothing here has been run against production. The dev
  numbers quoted in the comments (36 % of mentors on an off-list price, 98 % of
  completed requests with a live review capability) are from a dev database and
  may not describe production.
- **Retention windows and plan tiers** for Grafana Cloud and PostHog are account
  settings, recorded nowhere in this repository.
- **The RUB→USD rate used at import time** is not stored in the database, only in
  the migration run log. Without that log a recomputed price is uncertain — see
  `data-repair.md` §D2.4.
- **The `finalize-stuck-registrations` cron does not exist yet** on `main`.
  `data-repair.md` §D1.4 gives the trigger URL for once it ships, and the
  per-mentor call to use until then.

## Related runbooks

- [`../postgres-backup-restore.md`](../postgres-backup-restore.md) — dump layout,
  restore procedures, the quarterly drill
- [`../data-deletion.md`](../data-deletion.md) — erasure, and where personal data
  lives
- [`../mentor-migration.md`](../mentor-migration.md) — the getmentor.dev import
  that D2's recompute path depends on
