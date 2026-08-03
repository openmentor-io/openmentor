# Runbooks: 2026-08 audit diagnostics & repair

Operator-facing material for the 2026-08 audit remediation. The audit found
defects that may **already** have damaged production data; these files are what
you run against production to find out, and what you do about what you find.

They accompany the remediation plan (`docs/audit/2026-08-remediation-plan.md`,
which lands with the audit branch) — the plan is for whoever writes the code,
this directory is for whoever holds the production credentials.

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

## After the deploy: arm the backup alert

One remediation item does not finish when the code ships. P8 added the
`DatabaseBackupStale` rule to `grafana/alerting/alert-rules.yaml`, but nothing in
this repository applies that file to Grafana Cloud — the stack has no
Grafana-managed alert rules at all. Until an operator applies it, a nightly dump
can fail forever and the only signal is a container healthcheck that nothing off
the VM watches.

**Apply it after the `postgres-backup` sidecar is deployed, not before.** The rule
sets `noDataState: Alerting`, so landing it while the gauges it reads do not yet
exist pages immediately and keeps paging every 4h.

Procedure, prerequisites and the paused-first alternative:
[`../postgres-backup-restore.md`](../postgres-backup-restore.md) § *Operator step:
apply `DatabaseBackupStale`*, with the API/MCP details in
[`../../../grafana/README.md`](../../../grafana/README.md) § *Alert rules*.

## What the checks are

| Check | Detects | Repair |
|---|---|---|
| D1 / D1b | Mentors with `sort_order IS NULL` — they silently cannot log in, **and their public profile page cannot be rendered**. D1b separates lost registrations from imported profiles, because the repair for one damages the other. | `data-repair.md` §D1 |
| D2a–D2c | Prices overwritten with `Free` by an uncontrolled `<select>`, plus the exposure count. | `data-repair.md` §D2 |
| D2d | The same bug on the `experience` field. **Not in the audit plan** — found while writing these files. | `data-repair.md` §D2.1 |
| D3 | Whether the unescaped-email-template injection has been exercised in stored data. | investigation, not repair |
| D4 | Live review capabilities (completed requests with no review). | sizes the H4 redesign |

## Before you start

- **The report contains personal data** — names, email addresses, request text.
  `diagnostics.sh` writes it mode 600. Keep it off shared drives and delete it
  when you are done (`../data-deletion.md` explains why that matters here).
- **`diagnostics.sql` pins the session read-only** (`default_transaction_read_only`,
  plus statement and lock timeouts). A stray write aborts instead of landing. No
  `ANALYZE`, no `EXPLAIN ANALYZE`, no exclusive locks.
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
