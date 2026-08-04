# Runbook: Postgres Backup & Restore

**Trigger:** database loss/corruption, VM loss, a botched migration, or the quarterly restore drill. Production Postgres runs as the `postgres` container on the Hetzner VM (DECISIONS D2), defined in `infra/docker-compose.yml`.

## Architecture: three protection layers

| Layer | What | Protects against | Freshness |
|---|---|---|---|
| 1. Volume protection | Data in the Docker volume `openmentor-postgres-data`, declared `external` in compose (created by `deploy.sh`, never owned by compose) | `docker compose down -v`, stack rebuilds, image upgrades | live |
| 2. Hetzner VM auto-backups | Whole-VM snapshots taken by Hetzner (enable in Cloud Console → server → Backups) | VM/disk loss, fat-fingered host | Hetzner's schedule (daily) |
| 3. Nightly logical dumps | `postgres-backup` sidecar: `pg_dump -Fc` at `BACKUP_TIME` (default 03:30 UTC) → `s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/openmentor-YYYYMMDD-HHMM.dump`, pruned after `BACKUP_RETENTION_DAYS` (default 30) | provider loss, logical corruption that snapshots faithfully preserve | ≤ 24 h |

If `BACKUP_S3_BUCKET` is unset the sidecar keeps dumps in the local `openmentor-postgres-backups` volume and logs a loud warning — that is a degraded mode, not a configuration choice for production.

Force a dump at any time (also used by the drill):

```bash
docker exec openmentor-postgres-backup backup.sh once
docker logs openmentor-postgres-backup --tail 5   # expect a SUCCESS summary line
```

### How a broken backup gets noticed

The daemon loop swallows per-run failures on purpose — a transient error must
not kill the sidecar — so three things carry the signal instead:

| Layer | Where | Behaviour |
|---|---|---|
| Freshness marker | `/backups/.last_success` (and `.last_failure`) in the `openmentor-postgres-backups` volume, epoch seconds | Rewritten by every run. Absent = no dump has **ever** succeeded; nothing but a real dump creates it |
| Container healthcheck | `backup.sh healthcheck`, every 5 min | `unhealthy` once the last success is older than `BACKUP_MAX_AGE_HOURS` (26h). Deliberately does **not** roll back a deploy: `deploy-remote.sh` asserts only that the container is *running* |
| Grafana alert | `DatabaseBackupStale` (`grafana/alerting/alert-rules.yaml`), severity critical | **Not applied to the stack yet** — see the operator step below. Once applied: pages, per deployment, when the backup age passes the window the sidecar publishes (`openmentor_db_backup_max_age_seconds`, i.e. `BACKUP_MAX_AGE_HOURS`) **or** when the gauges disappear (`NoData=Alerting`). Panels: the "Postgres Backups" row on the `om-database-infra` dashboard — same expression and same per-deployment grouping, so the row and the page agree; unlike the rule, the dashboard is Git-Synced and needs no operator step |

The gauges reach Grafana Cloud as a Prometheus textfile: the sidecar writes
`openmentor_db_backup_last_{success,failure}_timestamp_seconds` and
`openmentor_db_backup_first_start_timestamp_seconds` into the
`openmentor-backup-metrics` volume, which Alloy mounts read-only and scrapes
(`prometheus.exporter.unix "backup_metrics"`). Publishing them is best-effort:
if the metrics volume is full or read-only the dump still succeeds and still
logs `SUCCESS`, and the gauges going stale is what raises the alarm.

```bash
# Is the sidecar happy right now?
docker inspect -f '{{.State.Health.Status}}' openmentor-postgres-backup
docker exec openmentor-postgres-backup backup.sh healthcheck
```

On a brand-new `/backups` volume the daemon stamps `.first_start` once — the
healthcheck and the alert stay quiet for 26 h from that stamp instead of paging
before the first scheduled dump can run. `.first_start` is never rewritten, so a
redeploy cannot reset the window on a pipeline that has been failing for weeks,
and `.last_success` is **not** seeded: while it is absent the healthcheck says
`no backup yet, Ns into the grace window` and the dashboard stat reads `never`.
Don't wait out the window on a first rollout — run `backup.sh once` and confirm
the `SUCCESS` line.

### Operator step: apply `DatabaseBackupStale` (once, after the sidecar deploys)

Only the healthcheck and the marker live on the VM, and **nothing off the VM
watches them** — `deploy-remote.sh` checks `.State.Status`, not health. Until
the alert is applied, a nightly dump can fail forever with no page. As of
2026-08-03 the Grafana Cloud stack has **no** Grafana-managed alert rules at all
(verified; see `grafana/README.md`), so this is a one-time operator action that
cannot be done from the repo.

Do it **after** the sidecar publishing the gauges is deployed, otherwise
`noDataState: Alerting` pages immediately (the live notification policy fans out
to telegram/slack/Discord and repeats every 4h):

```bash
# 1. the gauges must exist first
docker exec openmentor-postgres-backup backup.sh once     # expect SUCCESS
#    then in Grafana Explore (grafanacloud-prom), expect one series:
#    openmentor_db_backup_last_success_timestamp_seconds

# 2. apply the rule from the versioned file (folder uid repository-7b3d712,
#    group openmentor) — provisioning API with an editor token, or the
#    Grafana MCP `alerting_manage_rules` create. Details + the exact endpoints:
#    grafana/README.md § Alert rules
```

Then confirm: the rule appears under Alerting → Alert rules in folder
`openmentor` and evaluates to Normal, and `GET /api/v1/provisioning/alert-rules`
no longer returns `[]`.

## (a) Restore the latest dump into a fresh container/volume

Use this for logical corruption or to rebuild the DB from S3 on a new VM. On the VM, in `/opt/openmentor/infra`:

```bash
# 1. Stop writers (keep traefik up so LE certs don't churn)
docker compose stop backend worker migrate postgres-backup

# 2. Fetch the newest dump from S3
aws s3 ls s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/ | sort | tail -1
aws s3 cp s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/openmentor-YYYYMMDD-HHMM.dump /tmp/restore.dump

# 3. Move the (possibly corrupt) volume aside and create a fresh one
docker compose stop postgres && docker compose rm -f postgres
docker volume create openmentor-postgres-data-old
docker run --rm -v openmentor-postgres-data:/from -v openmentor-postgres-data-old:/to \
    alpine sh -c "cp -a /from/. /to/"
docker volume rm openmentor-postgres-data
docker volume create openmentor-postgres-data

# 4. Start an empty postgres (initializes from POSTGRES_* in .env) and restore.
#    SECURITY (H8): the dump carries `OWNER TO om_migrate` / GRANTs for om_api,
#    om_worker, om_backup and om_monitor_ro, and pg_dump does NOT carry roles.
#    Into a FRESH cluster, create them FIRST by applying that migration to the
#    empty database (verified: it is fine on an empty schema, and the restore
#    then reports 0 errors). Skip this and pg_restore prints ~23
#    `role "om_migrate" does not exist` errors and STILL EXITS 0 — the data lands,
#    owned by the restoring superuser with no om_* grants, so the restore looks
#    successful and the app then cannot connect:
docker compose up -d postgres           # wait for (healthy) in `docker compose ps`
docker exec -i openmentor-postgres psql -U openmentor -d openmentor -v ON_ERROR_STOP=1 \
    < ../api/migrations/000012_split_database_identities.up.sql
docker cp /tmp/restore.dump openmentor-postgres:/tmp/restore.dump
docker exec openmentor-postgres \
    pg_restore -U openmentor -d openmentor --clean --if-exists /tmp/restore.dump
docker exec openmentor-postgres rm /tmp/restore.dump
#    In a hurry, or restoring somewhere the roles are irrelevant (the drill in
#    section (c)): `pg_restore --no-owner --no-privileges` instead, which makes
#    everything owned by the restoring superuser. Do NOT do that on production —
#    it silently puts the app back on superuser-owned tables.

# 5. Re-set the role passwords and recreate the monitoring role (pg_dump captures
#    one database, not roles; the app role comes from POSTGRES_USER).
#    docs/runbooks/database-identities.md step 1 is the same procedure.
docker exec -it openmentor-postgres psql -U openmentor -c \
    "CREATE USER grafana_monitoring WITH PASSWORD '...'; GRANT pg_monitor TO grafana_monitoring; GRANT pg_read_all_stats TO grafana_monitoring; GRANT om_monitor_ro TO grafana_monitoring; GRANT CONNECT ON DATABASE openmentor TO grafana_monitoring;"
docker exec -it openmentor-postgres psql -U openmentor -d openmentor -c \
    "ALTER ROLE om_migrate LOGIN; ALTER ROLE om_api LOGIN; ALTER ROLE om_worker LOGIN; ALTER ROLE om_backup LOGIN;"
# then \password om_migrate / om_api / om_worker / om_backup in an interactive
# psql, so no cleartext reaches the server log or the process list

# 6. Bring the stack back and verify
docker compose up -d
docker exec openmentor-backend curl -sf http://localhost:8081/api/healthcheck
docker exec -it openmentor-postgres psql -U openmentor -c "SELECT count(*) FROM mentors;"

# 7. After a burn-in day, delete the -old volume
docker volume rm openmentor-postgres-data-old
```

Notes:

- `pg_dump -Fc` dumps a single database (`openmentor`), not roles. Recreate extra roles (steps 4-5) — the app role `openmentor` is created by the container from `POSTGRES_USER`, and `om_*` come from migration `000012`.
- `pg_restore --clean --if-exists` also works into a non-empty DB (e.g. rolling back a bad data migration without recreating the volume) — steps 2, 4(restore), 6 only.

## (b) Full VM-snapshot restore (Hetzner)

1. Hetzner Cloud Console → server → Backups/Snapshots → restore to the server, or create a new server from the snapshot (new IP → update the Cloudflare A record).
2. The snapshot is **crash-consistent**: it captures the volume as if the machine lost power. Postgres handles this by design — on first start it replays WAL automatically. Watch `docker logs openmentor-postgres` for `redo done` / `database system is ready`.
3. `cd /opt/openmentor/infra && docker compose up -d`, then run the deploy health checks (or just `./deploy.sh infra` from a workstation to re-push `.env` and verify).
4. Anything written between the snapshot and the failure is lost — if the nightly dump is newer than the snapshot, follow (a) on top to close the gap.

## (c) Quarterly restore-test procedure

Do this every quarter (put it in the ops calendar); a backup that has never been restored is a hope, not a backup. On a workstation or a scratch VM — never against production:

```bash
# 1. Take a fresh dump (or use last night's from S3)
aws s3 cp s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/<latest>.dump /tmp/drill.dump

# 2. Throwaway postgres of the same major version
docker run -d --name pg-drill -e POSTGRES_USER=openmentor \
    -e POSTGRES_PASSWORD=drill -e POSTGRES_DB=openmentor postgres:16.14-alpine
docker cp /tmp/drill.dump pg-drill:/tmp/drill.dump
# --no-owner --no-privileges: the drill only proves the DATA restores, and the
# om_* roles (H8) do not exist in a throwaway cluster. Without these flags every
# owner/GRANT line errors and the exit code hides a real failure.
docker exec pg-drill pg_restore -U openmentor -d openmentor \
    --no-owner --no-privileges /tmp/drill.dump

# 3. Sanity queries: row counts and recency
docker exec pg-drill psql -U openmentor -c "SELECT count(*) FROM mentors;"
docker exec pg-drill psql -U openmentor -c \
    "SELECT max(created_at) FROM client_requests;"   # should be ~last 24h

# 4. Clean up and log the drill
docker rm -f pg-drill && rm /tmp/drill.dump
```

Record date, dump filename, row counts and time-to-restore in the ops tracker. Also check the sidecar is alive: `docker inspect -f '{{.State.Health.Status}}' openmentor-postgres-backup` must report `healthy`, and `docker logs openmentor-postgres-backup --tail 3` must show a SUCCESS line less than 24 h old.

## (d) RPO / RTO

- **Current (nightly dumps):** RPO ≤ 24 h (last nightly dump), RTO ≈ 30 min (procedure (a): fetch, fresh volume, `pg_restore`, health checks). VM snapshot restores are similar RTO with Hetzner-schedule RPO.
- **Upgrade path — wal-g (documented, NOT implemented):** continuous WAL archiving to S3 gives ~minutes RPO and point-in-time recovery. Sketch:
  1. Extend `postgres-backup/Dockerfile` (or the postgres image) with the `wal-g` binary.
  2. Postgres config: `archive_mode=on`, `archive_command='wal-g wal-push %p'`, plus `WALG_S3_PREFIX=s3://<bucket>/walg` and AWS creds in the environment.
  3. Nightly `wal-g backup-push $PGDATA` base backups replace/augment the pg_dump job; `wal-g delete retain FULL 7` for retention.
  4. Restore: `wal-g backup-fetch` into an empty volume + `recovery_target_time` in `postgresql.conf` for PITR, then start the container and let it replay WAL.
  5. Keep the nightly `pg_dump` anyway — logical dumps survive cross-version moves and are the managed-PG import format.
- **Scale path (D2):** managed Postgres (Neon/RDS) — import the latest dump, then swap `DATABASE_URL` to the managed host with `sslmode=verify-full` (the Go API verifies against the CA in its `certs/` directory; see `api/pkg/db/pool.go`). Backup ownership then moves to the provider.

## (e) Common failures

| Symptom | Cause | Fix |
|---|---|---|
| `external volume "openmentor-postgres-data" not found` on `up` | The volume is external so compose refuses to create it — first boot on a fresh VM/workstation, or someone deleted it | `docker volume create openmentor-postgres-data` (deploy.sh/deploy-dev.sh/rollback.sh do this automatically) — then check whether a restore per (a)/(b) is needed |
| Volume exists but the DB is empty after `up` | Fresh volume: the container initialized a brand-new cluster | Restore the latest dump per (a) |
| `password authentication failed for user "openmentor"` from backend/worker/migrate | `POSTGRES_PASSWORD` was rotated in `.env` — the container only applies it on **first initialization**; the running cluster keeps the old password, or `DATABASE_URL` wasn't updated to match | Either update the cluster to the new value: `docker exec -it openmentor-postgres psql -U openmentor -c "ALTER USER openmentor WITH PASSWORD '<new>';"` — or fix `DATABASE_URL`/`POSTGRES_PASSWORD` so they agree, then `docker compose up -d` |
| Sidecar logs `FAILURE ... error=pg_dump_failed` | postgres down/unhealthy, or creds mismatch after rotation (sidecar uses `POSTGRES_*` from the same `.env`) | Check `docker compose ps` / postgres logs; re-run `backup.sh once` after fixing |
| Sidecar logs `FAILURE ... error=s3_upload_failed` (dump kept locally) | Bad/wrong `BACKUP_AWS_*` creds (there is **no** fallback to `S3_STORAGE_*` — SECURITY M12), wrong region, or bucket policy | Fix creds/bucket; the dump is still in the `openmentor-postgres-backups` volume — upload manually with `aws s3 cp` |
| Sidecar container is `restarting` and logs `FATAL: BACKUP_S3_BUCKET is set but BACKUP_AWS_ACCESS_KEY_ID / ... are not` | `BACKUP_S3_BUCKET` set with empty dedicated creds. Under `restart: always` this loops forever, and `deploy-remote.sh` reads a `restarting` container as unhealthy → **auto-rollback of a healthy deploy** | Fill `BACKUP_AWS_ACCESS_KEY_ID`/`BACKUP_AWS_SECRET_ACCESS_KEY` in `.env.production`, or clear `BACKUP_S3_BUCKET` to accept local-only backups; then redeploy |
| Sidecar logs the loud `BACKUP_S3_BUCKET is not set` warning in production | Off-site backups not configured | Set `BACKUP_S3_BUCKET` **and** the dedicated `BACKUP_AWS_*` creds in `.env.production`, then redeploy |
| `pg_restore: error: unsupported version` | Dump made by a newer pg_dump than the restoring server | Restore into the same or newer major (`postgres:16.14-alpine` or later) |

## Notes

- Backups contain personal data: the S3 backup bucket must be private, encrypted (SSE-S3 is fine) and in the EU region; deletion requests age out of dumps with `BACKUP_RETENTION_DAYS` — this is stated in the privacy policy (see `data-deletion.md`).
- The `postgres` container publishes no ports; all admin access is `docker exec -it openmentor-postgres psql -U openmentor` on the VM.
- Config source of truth: `infra/docker-compose.yml`, `infra/postgres-backup/backup.sh`, and the `BACKUP_*`/`POSTGRES_*` sections of the env templates.
