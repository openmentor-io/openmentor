# Runbook: Split Database Identities (per-process Postgres roles)

**Trigger:** the one-time switch of migrate / backend / worker / the backup
sidecar off the Postgres bootstrap superuser and onto their own roles
(SECURITY H8, `DECISIONS.md` D67). Also the reference for adding another role
later.

**Who can run this:** someone with `infra/.env.production` and SSH access to the
VM. Every step is executed by a human; nothing here is automated.

**Time:** ~20 minutes for the four switches, but they are independent — stopping
after any step leaves a consistent system.

## What this is not

- **Not a credential rotation.** `POSTGRES_PASSWORD` and the existing
  `DATABASE_URL` are not changed, re-issued or retired here. The owner declined
  the one-off rotation (D56) and this does not reintroduce one: it *adds*
  identities. `secret-rotation.md` remains the runbook for rotation.
- **Not a flag day.** Every DSN variable falls back to today's `DATABASE_URL`
  when it is absent, so the migration that creates the roles changes nothing on
  its own, and each service moves on its own schedule.
- **Not the end of the superuser.** `POSTGRES_USER` still bootstraps the
  container, still owns nothing that matters after step 1, and is still what
  `infra/db.sh` connects as for admin work. Revoking privileges from it is a
  separate decision (see "Afterwards").

## Preconditions

| # | Check | Why |
|---|---|---|
| 1 | The H8 change is deployed: `cd infra && ./db.sh -c "SELECT rolname, rolcanlogin FROM pg_roles WHERE rolname LIKE 'om\_%' ORDER BY 1"` lists `om_api`, `om_backup`, `om_migrate`, `om_monitor_ro`, `om_worker`, all with `rolcanlogin = f` | migration `000012` is what creates them; without it every step below fails at authentication |
| 2 | `./db.sh -c "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind='r' AND c.relowner='om_migrate'::regrole"` returns the table count (9 at the time of writing) | ownership moved; step 5 depends on it |
| 3 | A fresh backup: `docker exec openmentor-postgres-backup /usr/local/bin/backup.sh once` shows `SUCCESS` | you are about to change the credentials the backup path uses |
| 4 | `cp .env.production .env.production.pre-h8` (kept OUTSIDE the repo) | the rollback for every step is an `.env` edit |
| 5 | Nobody else is deploying | each step ends in `./deploy.sh infra`, which converges the whole stack |

## Step 1 — give the roles a password and LOGIN

The roles ship `NOLOGIN` with no password precisely so that deploying the
migration cannot change how anything authenticates. Turning that on is the first
deliberate act.

Generate four passwords with `openssl rand -base64 32` and store them in the
password manager next to `POSTGRES_PASSWORD`.

**Do** — from `infra/`, open a psql session on the VM and use `\password`, not an
`ALTER ROLE ... PASSWORD '<value>'` string:

```bash
cd infra && ./db.sh
```

```
ALTER ROLE om_migrate LOGIN;
ALTER ROLE om_api     LOGIN;
ALTER ROLE om_worker  LOGIN;
ALTER ROLE om_backup  LOGIN;
\password om_migrate
\password om_api
\password om_worker
\password om_backup
```

`\password` hashes the value in psql and sends only the SCRAM verifier, so the
cleartext never reaches the server log, the VM's process list or your shell
history. `ALTER ROLE ... LOGIN` carries no secret, so it is safe to type or to
pass through `./db.sh -c`.

**Verify:** `./db.sh -c "SELECT rolname, rolcanlogin FROM pg_roles WHERE rolname LIKE 'om\_%' ORDER BY 1"`
shows `t` for the four roles (`om_monitor_ro` stays `f` — it is a group role,
nothing logs in as it).

**Roll back:** `./db.sh -c "ALTER ROLE om_api NOLOGIN"` (and so on). Nothing uses
the roles yet, so this step is inert until step 2.

## Steps 2-5 — move one process at a time

Each step is the same shape: add ONE line to `infra/.env.production`, sync it,
verify, and move on. `./deploy.sh infra` re-uploads `.env.production` as the VM's
`.env` and runs `docker compose up -d`, which recreates only the service whose
rendered configuration changed.

The single most useful verification, after every step — who is actually
connected:

```bash
cd infra && ./db.sh -c \
  "SELECT usename, count(*) FROM pg_stat_activity WHERE datname = current_database() GROUP BY 1 ORDER BY 1"
```

Do them in this order. It is ascending blast radius, and it deliberately leaves
`migrate` for last: its failure mode only appears at the *next* deploy, so you
want the other three already proven by then.

| # | Service | Line to add to `.env.production` | Verify | Roll back |
|---|---|---|---|---|
| 2 | `postgres-backup` | `BACKUP_POSTGRES_USER=om_backup`<br>`BACKUP_POSTGRES_PASSWORD=<pw>` | `./deploy.sh infra --yes`, then `docker exec openmentor-postgres-backup /usr/local/bin/backup.sh once` prints `SUCCESS`, and `docker inspect -f '{{.State.Health.Status}}' openmentor-postgres-backup` says `healthy` | delete both lines, `./deploy.sh infra --yes`. A failed dump is not urgent — the sidecar stays `healthy` until `BACKUP_MAX_AGE_HOURS` (26h) |
| 3 | `worker` | `WORKER_DATABASE_URL=postgres://om_worker:<pw>@postgres:5432/openmentor?sslmode=disable` | `./deploy.sh infra --yes`, then `docker logs --since 2m openmentor-worker` has no connection errors, `docker inspect -f '{{.State.Health.Status}}' openmentor-worker` is `healthy`, and `om_worker` appears in the query above | delete the line, `./deploy.sh infra --yes`. Impact while broken: async jobs and transactional email stop; the site keeps serving |
| 4 | `backend` | `API_DATABASE_URL=postgres://om_api:<pw>@postgres:5432/openmentor?sslmode=disable` | `./deploy.sh infra --yes` (its own step 9 fails the deploy if `https://$DOMAIN/api/healthcheck` is not 200), then load the catalog and one mentor profile in a browser, and check `om_api` in the query above | delete the line, `./deploy.sh infra --yes`. This is the user-facing one: if the container will not start, `docker compose up -d backend` after the edit is faster than a full deploy |
| 5 | `migrate` | `MIGRATE_DATABASE_URL=postgres://om_migrate:<pw>@postgres:5432/openmentor?sslmode=disable` | `./deploy.sh infra --yes`, then `docker logs openmentor-migrate` ends with `Database migrations completed successfully` and exit code 0 (`docker inspect -f '{{.State.ExitCode}}' openmentor-migrate`) | delete the line, `./deploy.sh infra --yes` |

Notes that save time:

- `docker compose up -d <service>` may re-run the `migrate` container as a
  dependency. That is harmless — with nothing to apply, `migrate` exits 0.
- A wrong password shows up as `password authentication failed for user "om_api"`
  in `docker logs`, not as a hang.
- If `migrate` ever fails on a *privilege* error (`must be owner of ...`,
  `permission denied for schema public`), the migration is doing DDL the migrator
  role was not granted. Roll back step 5, land the grant as a new additive
  migration, then retry — do not grant it by hand on the VM, or the next fresh
  database will not have it.

## Step 6 — narrow the monitoring role (optional, separate)

`grafana_monitoring` currently holds `pg_read_all_data`, i.e. SELECT on every
mentor and client email. Migration `000012` created `om_monitor_ro` with the
scoped read set (`tags`, `mentor_tags`, `mentor_slug_history`,
`migration_intents`, `schema_migrations`) but granted it to nobody: this narrows
a LIVE integration, so it is a deliberate step, not a side effect of a deploy.

**Do:**

```bash
cd infra
./db.sh -c "GRANT om_monitor_ro TO grafana_monitoring"
./db.sh -c "REVOKE pg_read_all_data FROM grafana_monitoring"
```

**Verify** in Grafana Cloud → Observability → Databases, within ~5 minutes:
query stats, wait events and normalized query text still populate (they come
from `pg_monitor`, which is untouched).

**Expect to lose** the parts that need to read rows: EXPLAIN plans for queries
touching `mentors` / `client_requests` / `moderators` / `reviews`, and schema
sampling on those tables. That is the trade being made — decide, do not
discover.

**Roll back:** `./db.sh -c "GRANT pg_read_all_data TO grafana_monitoring"`. No
restart is needed; privileges apply to the next statement.

`database-observability.md` documents the same grant set from the monitoring
side.

## Afterwards

What is deliberately NOT done here, and why:

- **Nothing is revoked from `POSTGRES_USER`.** It is the bootstrap superuser of
  the container; removing privileges from it would break `initdb`-adjacent
  behaviour and `db.sh`. What has changed is that nothing *routine* uses it any
  more, which is the point.
- **`DATABASE_URL` stays in `.env.production`** as the fallback for all four
  services. Deleting it would turn a single missing variable into an outage
  instead of a reversion.
- **Restores need the roles to exist.** A `pg_dump` of this database now carries
  `OWNER TO om_migrate` lines. Restoring into a *fresh* cluster therefore either
  needs the roles created first (apply `000012`) or `pg_restore --no-owner` —
  see `postgres-backup-restore.md`.
