# Runbook: Postgres Backup & Restore

**Trigger:** database loss/corruption, VM loss, a botched migration, or the quarterly restore drill. Production Postgres runs as the `postgres` container on the Hetzner VM (DECISIONS D2), defined in `openmentor-infra/docker-compose.yml`.

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

## (a) Restore the latest dump into a fresh container/volume

Use this for logical corruption or to rebuild the DB from S3 on a new VM. On the VM, in `/opt/openmentor-infra`:

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

# 4. Start an empty postgres (initializes from POSTGRES_* in .env) and restore
docker compose up -d postgres           # wait for (healthy) in `docker compose ps`
docker cp /tmp/restore.dump openmentor-postgres:/tmp/restore.dump
docker exec openmentor-postgres \
    pg_restore -U openmentor -d openmentor --clean --if-exists /tmp/restore.dump
docker exec openmentor-postgres rm /tmp/restore.dump

# 5. Recreate the monitoring role (pg_dump captures one database, not roles;
#    the app role comes from POSTGRES_USER, extra roles must be recreated)
docker exec -it openmentor-postgres psql -U openmentor -c \
    "CREATE USER grafana_monitoring WITH PASSWORD '...'; GRANT pg_monitor TO grafana_monitoring; GRANT CONNECT ON DATABASE openmentor TO grafana_monitoring;"

# 6. Bring the stack back and verify
docker compose up -d
docker exec openmentor-backend curl -sf http://localhost:8081/api/healthcheck
docker exec -it openmentor-postgres psql -U openmentor -c "SELECT count(*) FROM mentors;"

# 7. After a burn-in day, delete the -old volume
docker volume rm openmentor-postgres-data-old
```

Notes:

- `pg_dump -Fc` dumps a single database (`openmentor`), not roles. Recreate extra roles (step 5) — the app role `openmentor` is created by the container from `POSTGRES_USER`.
- `pg_restore --clean --if-exists` also works into a non-empty DB (e.g. rolling back a bad data migration without recreating the volume) — steps 2, 4(restore), 6 only.

## (b) Full VM-snapshot restore (Hetzner)

1. Hetzner Cloud Console → server → Backups/Snapshots → restore to the server, or create a new server from the snapshot (new IP → update the Cloudflare A record).
2. The snapshot is **crash-consistent**: it captures the volume as if the machine lost power. Postgres handles this by design — on first start it replays WAL automatically. Watch `docker logs openmentor-postgres` for `redo done` / `database system is ready`.
3. `cd /opt/openmentor-infra && docker compose up -d`, then run the deploy health checks (or just `./deploy.sh --skip-frontend --skip-backend` from a workstation to re-push `.env` and verify).
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
docker exec pg-drill pg_restore -U openmentor -d openmentor /tmp/drill.dump

# 3. Sanity queries: row counts and recency
docker exec pg-drill psql -U openmentor -c "SELECT count(*) FROM mentors;"
docker exec pg-drill psql -U openmentor -c \
    "SELECT max(created_at) FROM client_requests;"   # should be ~last 24h

# 4. Clean up and log the drill
docker rm -f pg-drill && rm /tmp/drill.dump
```

Record date, dump filename, row counts and time-to-restore in the ops tracker. Also check the sidecar is alive: `docker logs openmentor-postgres-backup --tail 3` must show a SUCCESS line less than 24 h old (alerting on its absence is a good follow-up).

## (d) RPO / RTO

- **Current (nightly dumps):** RPO ≤ 24 h (last nightly dump), RTO ≈ 30 min (procedure (a): fetch, fresh volume, `pg_restore`, health checks). VM snapshot restores are similar RTO with Hetzner-schedule RPO.
- **Upgrade path — wal-g (documented, NOT implemented):** continuous WAL archiving to S3 gives ~minutes RPO and point-in-time recovery. Sketch:
  1. Extend `postgres-backup/Dockerfile` (or the postgres image) with the `wal-g` binary.
  2. Postgres config: `archive_mode=on`, `archive_command='wal-g wal-push %p'`, plus `WALG_S3_PREFIX=s3://<bucket>/walg` and AWS creds in the environment.
  3. Nightly `wal-g backup-push $PGDATA` base backups replace/augment the pg_dump job; `wal-g delete retain FULL 7` for retention.
  4. Restore: `wal-g backup-fetch` into an empty volume + `recovery_target_time` in `postgresql.conf` for PITR, then start the container and let it replay WAL.
  5. Keep the nightly `pg_dump` anyway — logical dumps survive cross-version moves and are the managed-PG import format.
- **Scale path (D2):** managed Postgres (Neon/RDS) — import the latest dump, then swap `DATABASE_URL` to the managed host with `sslmode=verify-full` (openmentor-api verifies against the CA in its `certs/` directory; see `pkg/db/pool.go`). Backup ownership then moves to the provider.

## (e) Common failures

| Symptom | Cause | Fix |
|---|---|---|
| `external volume "openmentor-postgres-data" not found` on `up` | The volume is external so compose refuses to create it — first boot on a fresh VM/workstation, or someone deleted it | `docker volume create openmentor-postgres-data` (deploy.sh/rollback.sh/dev.sh do this automatically) — then check whether a restore per (a)/(b) is needed |
| Volume exists but the DB is empty after `up` | Fresh volume: the container initialized a brand-new cluster | Restore the latest dump per (a) |
| `password authentication failed for user "openmentor"` from backend/worker/migrate | `POSTGRES_PASSWORD` was rotated in `.env` — the container only applies it on **first initialization**; the running cluster keeps the old password, or `DATABASE_URL` wasn't updated to match | Either update the cluster to the new value: `docker exec -it openmentor-postgres psql -U openmentor -c "ALTER USER openmentor WITH PASSWORD '<new>';"` — or fix `DATABASE_URL`/`POSTGRES_PASSWORD` so they agree, then `docker compose up -d` |
| Sidecar logs `FAILURE ... error=pg_dump_failed` | postgres down/unhealthy, or creds mismatch after rotation (sidecar uses `POSTGRES_*` from the same `.env`) | Check `docker compose ps` / postgres logs; re-run `backup.sh once` after fixing |
| Sidecar logs `FAILURE ... error=s3_upload_failed` (dump kept locally) | Bad/absent AWS creds (`BACKUP_AWS_*`, falling back to `S3_STORAGE_*`), wrong region, or bucket policy | Fix creds/bucket; the dump is still in the `openmentor-postgres-backups` volume — upload manually with `aws s3 cp` |
| Sidecar logs the loud `BACKUP_S3_BUCKET is not set` warning in production | Off-site backups not configured | Set `BACKUP_S3_BUCKET` (+ creds) in `.env.production` and redeploy |
| `pg_restore: error: unsupported version` | Dump made by a newer pg_dump than the restoring server | Restore into the same or newer major (`postgres:16.14-alpine` or later) |

## Notes

- Backups contain personal data: the S3 backup bucket must be private, encrypted (SSE-S3 is fine) and in the EU region; deletion requests age out of dumps with `BACKUP_RETENTION_DAYS` — this is stated in the privacy policy (see `data-deletion.md`).
- The `postgres` container publishes no ports; all admin access is `docker exec -it openmentor-postgres psql -U openmentor` on the VM.
- Config source of truth: `openmentor-infra/docker-compose.yml`, `openmentor-infra/postgres-backup/backup.sh`, and the `BACKUP_*`/`POSTGRES_*` sections of the env templates.
