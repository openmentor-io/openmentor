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

0. **[`pre-deploy-checks.md`](pre-deploy-checks.md)** — run these **before** the
   remediation deploy. The remediation tightens validation that existing rows
   predate, so each check counts how many real people meet the new rule on their
   next save. They are counts, not gates: none of them blocks the deploy, all of
   them decide whether you warn someone first.

1. **`diagnostics.sh`** — run the checks and write a report.
   ```bash
   ./diagnostics.sh --dry-run     # confirm preconditions, connect to nothing
   ./diagnostics.sh               # on the VM; psql inside openmentor-postgres
   ```
   `--dry-run` exits 3 when something the real run needs is missing (no docker,
   container not running, no psql, report path already taken), so it is safe to
   gate on in a script.
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
the regression suite for the files above, run by `Checks / required-checks`. It
also pins the operator instructions in the remediation plan against these
runbooks, so a plan edit runs it too. Run it before changing any of them:

```bash
./diagnostics_test.sh          # shell tier: no database needed
# full run, against a THROWAWAY database. It creates om_diag_test_<pid>_<random>
# and drops only what it created, so it cannot delete a database that was already
# there — but the tier still writes, so do not point it at anything you care about:
docker run -d --name pg-diagtest -e POSTGRES_USER=openmentor \
    -e POSTGRES_PASSWORD=scratch -e POSTGRES_DB=openmentor \
    -p 55444:5432 postgres:16.14-alpine
PGHOST=127.0.0.1 PGPORT=55444 PGUSER=openmentor PGPASSWORD=scratch \
    PGDATABASE=openmentor OM_DIAG_TEST_DB=1 ./diagnostics_test.sh
docker rm -f pg-diagtest
```

## After the deploy: arm the backup alerts

One remediation item does not finish when the code ships. P8 added
`DatabaseBackupStale` **and** `DatabaseBackupPipelineAbsent` to
`grafana/alerting/alert-rules.yaml`, but nothing in this repository applies that
file to Grafana Cloud — the stack has no Grafana-managed alert rules at all.
Until an operator applies them, the only signals a failing nightly dump produces
are the sidecar's container healthcheck and the `exit 2` a deploy returns on
seeing it unhealthy — both of which need someone to be deploying or looking.

Note the split: the **dashboards** under `grafana/dashboards` are Git-Synced
hourly, so the backup panels arrive by themselves once this merges. The **alert
rules are not** — they are the manual step below.

**Apply them after the `postgres-backup` sidecar is deployed, not before.**
`DatabaseBackupStale` sets `noDataState: Alerting` and
`DatabaseBackupPipelineAbsent` is an `absent()` query, so both page immediately
if they land while the gauges they read do not yet exist, and the live
notification policy then repeats every 4h.

Procedure, prerequisites and the paused-first alternative:
[`../postgres-backup-restore.md`](../postgres-backup-restore.md) § *Operator step:
apply `DatabaseBackupStale`*, with the API/MCP details in
[`../../../grafana/README.md`](../../../grafana/README.md) § *Alert rules*.

## What the checks are

| Check | Detects | Repair |
|---|---|---|
| D1 / D1b | Mentors with `sort_order IS NULL` — they silently cannot log in, **and their public profile page cannot be rendered**. D1b separates lost registrations from imported profiles, because the repair for one damages the other. | `data-repair.md` §D1 |
| D2a–D2c | Prices overwritten with `Free` by an uncontrolled `<select>`, plus the exposure count. | `data-repair.md` §D2 |
| D2d | The same bug on the `experience` field. Found while writing these files; the plan originally called `experience` unaffected and has been corrected (plan §4.1). | `data-repair.md` §D2.1 |
| D3 | Whether the unescaped-email-template injection has been exercised in stored data. Covers all seven fields that reach an SES template: request description / name / **preferred contact**, mentor name / **price** / calendar URL, and mentor review. The calendar URL is checked for its scheme **and** for characters that break out of `href="…"` — an `https://` URL can still carry markup. | investigation, not repair |
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
- **The `finalize-stuck-registrations` cron ships with the remediation**, so it
  reconciles nothing until that worker image is deployed. `data-repair.md` §D1.4
  gives its trigger URL and the per-mentor call to use before the deploy.

## Related runbooks

- [`../postgres-backup-restore.md`](../postgres-backup-restore.md) — dump layout,
  restore procedures, the quarterly drill
- [`../data-deletion.md`](../data-deletion.md) — erasure, and where personal data
  lives
- [`../mentor-migration.md`](../mentor-migration.md) — the getmentor.dev import
  that D2's recompute path depends on
