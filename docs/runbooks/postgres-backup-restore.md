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
| Grafana alert | `DatabaseBackupStale` (`grafana/alerting/alert-rules.yaml`), severity critical | **Live since 2026-08-04.** Pages, per deployment, when the backup age passes the window the sidecar publishes (`openmentor_db_backup_max_age_seconds`, i.e. `BACKUP_MAX_AGE_HOURS`) **or** when the gauges disappear (`NoData=Alerting`); `DatabaseBackupPipelineAbsent` covers one deployment going silent while another keeps publishing. Panels: the "Postgres Backups" row on the `om-database-infra` dashboard — same expression and same per-deployment grouping, so the row and the page agree. The dashboard is Git-Synced hourly; **the rules are not**, so an edit to `alert-rules.yaml` is desired state until an operator re-applies the group |

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

### Re-applying the rules after an edit to `alert-rules.yaml`

Only the healthcheck and the freshness marker live on the VM, and nothing on the
VM watches them from outside — `deploy-remote.sh` checks the container's health,
not the dumps' age off-box. The Grafana rules are what close that gap, and all
14 of them have been **live since 2026-08-04** (applied after the 2026-08-04
deploy outage paged nobody). What remains an operator step is *re-applying* them:
dashboards Git-Sync from `grafana/dashboards` hourly, alert rules do **not** sync
at all, so any edit to `grafana/alerting/alert-rules.yaml` is desired state until
someone pushes it.

```bash
# 1. For a NEW rule with noDataState: Alerting, the series must exist first, or
#    it pages the moment it lands (the live policy fans out to
#    telegram/slack/Discord and repeats every 4h).
docker exec openmentor-postgres-backup backup.sh once     # expect SUCCESS
#    then in Grafana Explore (grafanacloud-prom), expect one series:
#    openmentor_db_backup_last_success_timestamp_seconds

# 2. PUT the whole group — atomic, and it matches the file exactly:
#    PUT /api/v1/provisioning/folder/fd2fpl/rule-groups/openmentor
#    with an editor/admin service-account token and X-Disable-Provenance: true.
#    Folder "OpenMentor", uid fd2fpl — NOT the Git Sync folder repository-7b3d712:
#    Grafana refuses to store alert rules in a Git-Sync-managed folder.
#    Details and the exact body shape: grafana/README.md § Alert rules
```

Then confirm the group under Alerting → Alert rules in the **OpenMentor** folder
evaluates to Normal, and that
`GET /api/v1/provisioning/folder/fd2fpl/rule-groups/openmentor` returns what the
file says.

## How to reach S3 — read this before (a) or (c)

**There is no `aws` CLI and no AWS credentials on the VM, by design.** The deploy
path pipes short-lived ECR tokens in over ssh stdin (`infra/deploy-remote.sh`)
and `.env.production.example` says it outright: "The VM needs NO AWS
credentials". The backup keys are handed to exactly one container —
`BACKUP_AWS_ACCESS_KEY_ID` / `BACKUP_AWS_SECRET_ACCESS_KEY` /
`BACKUP_AWS_REGION` on `openmentor-postgres-backup` in
`infra/docker-compose.yml` — because those keys can delete every dump
(SECURITY M12). That container is also the only one carrying `aws-cli`
(`infra/postgres-backup/Dockerfile` installs it).

**So every S3 call in this runbook runs inside the backup sidecar**, exactly as
`audit-2026-08/data-repair.md` §3 does. `backup.sh` maps `BACKUP_AWS_*` to the
`AWS_*` names inside its own process only, so a `docker exec` has to do that
mapping itself. The single quotes are load-bearing: the variables expand *in the
container*, so no key reaches your shell history or the host process list. It is
also why `$BACKUP_S3_BUCKET` works there and not in your VM shell — those values
live in `/opt/openmentor/infra/.env`, which nothing sources for you.

```bash
# Reusable: list what is in the bucket.
docker exec openmentor-postgres-backup sh -c '
  export AWS_ACCESS_KEY_ID="$BACKUP_AWS_ACCESS_KEY_ID" \
         AWS_SECRET_ACCESS_KEY="$BACKUP_AWS_SECRET_ACCESS_KEY" \
         AWS_DEFAULT_REGION="${BACKUP_AWS_REGION:-eu-central-1}"
  aws s3 ls "s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/"
' | sort
```

If you would rather work on a workstation, that needs **your own** read
credentials for the backup bucket — do not copy the container's keys out.
Otherwise fetch to the VM as below and `scp` the file to yourself.

`df -h` first: a restore lands up to three copies of the dump on the VM (the
sidecar volume, host `/tmp`, and the restored cluster).

### Fetching a dump out of the sidecar (used by both (a) and (c))

```bash
# 1. Pull the object into the sidecar's /backups volume. The name deliberately
#    does NOT match 'openmentor-*.dump', so the retention pruner leaves it alone.
docker exec openmentor-postgres-backup sh -c '
  export AWS_ACCESS_KEY_ID="$BACKUP_AWS_ACCESS_KEY_ID" \
         AWS_SECRET_ACCESS_KEY="$BACKUP_AWS_SECRET_ACCESS_KEY" \
         AWS_DEFAULT_REGION="${BACKUP_AWS_REGION:-eu-central-1}"
  aws s3 cp "s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/openmentor-YYYYMMDD-HHMM.dump" \
            /backups/restore-candidate.dump
'

# 2. Copy it to the host — but NOT with a bare `docker cp`. `docker cp` behaves
#    like `cp -a` and applies the mode from the tar header it reads out of the
#    container, so the sidecar's 0644 lands on the host copy: a complete
#    production database dump readable by every local user on the VM. `umask 077`
#    does NOT fix that. Measured twice — once for data-repair.md and again on
#    2026-08-04 for this runbook: `docker cp` of a 0644 file produced 0644 on the
#    host under umask 077, and only the explicit chmod gave 600. So refuse a path
#    we did not create, create it under 077, and set the mode ourselves.
if [ -e /tmp/restore.dump ] || [ -L /tmp/restore.dump ]; then
  echo "refusing: /tmp/restore.dump already exists — its mode and owner are not ours" >&2
else
  ( umask 077
    docker cp openmentor-postgres-backup:/backups/restore-candidate.dump /tmp/restore.dump )
  chmod 600 /tmp/restore.dump
  ls -l /tmp/restore.dump   # must print -rw------- before you continue
fi
```

Delete both copies when you are done — `rm -f /tmp/restore.dump` and
`docker exec openmentor-postgres-backup rm -f /backups/restore-candidate.dump`.

## (a) Restore the latest dump into a fresh container/volume

Use this for logical corruption or to rebuild the DB from S3 on a new VM. On the VM, in `/opt/openmentor/infra`:

> **On a brand-new VM, deploy first.** The sidecar *is* the S3 client, so it has
> to exist before you can fetch anything: provision per
> `provisioning.md` §4, then `cd infra && ./deploy.sh all` from a workstation
> (`deploy.sh` creates the `openmentor-postgres-data` volume, uploads `.env` and
> starts the sidecar). The stack comes up on an empty database — that is fine,
> it is what you are about to replace. If you have no working stack at all,
> fetching the dump needs your own read credentials for the bucket on a
> workstation, then `scp`; the container's keys must not be copied out.

```bash
# 1. Stop writers (keep traefik up so LE certs don't churn). NOT the backup
#    sidecar — it is how you reach S3 in step 2, and stopping it also stops the
#    only thing that would tell you the pipeline is alive.
docker compose stop backend worker migrate

# 2. Find and fetch the dump: the two blocks above ("Fetching a dump out of the
#    sidecar"). You want /tmp/restore.dump at mode 600 before continuing.
#    Stop the sidecar only now, so it cannot start a scheduled dump against the
#    cluster you are about to replace:
docker compose stop postgres-backup

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
3. `cd /opt/openmentor/infra && docker compose up -d`, then run the deploy health checks (or just `./deploy.sh infra` from a workstation to re-push `.env` and verify).
4. Anything written between the snapshot and the failure is lost — if the nightly dump is newer than the snapshot, follow (a) on top to close the gap.

## (c) Quarterly restore-test procedure

Do this every quarter (put it in the ops calendar); a backup that has never been restored is a hope, not a backup. On a workstation or a scratch VM — never against production:

The drill runs **on the VM**, because that is where the only credentials for the
backup bucket are (see "How to reach S3" above). Production is protected by the
throwaway container having no `--network`, so it cannot reach the production
database at all — same guard `audit-2026-08/data-repair.md` uses. Nothing here
touches the production stack.

```bash
# 1. Force a fresh dump so the drill covers tonight's pipeline, not last week's
docker exec openmentor-postgres-backup backup.sh once
docker logs openmentor-postgres-backup --tail 5   # expect a SUCCESS summary line

# 2. Fetch it: the two blocks under "Fetching a dump out of the sidecar" above.
#    /tmp/restore.dump, mode 600.

# 3. Throwaway postgres of the same major version, on no network
docker run -d --name pg-drill --network none \
    -e POSTGRES_USER=openmentor \
    -e POSTGRES_PASSWORD="$(openssl rand -hex 16)" \
    -e POSTGRES_DB=openmentor postgres:16.14-alpine
until docker exec pg-drill pg_isready -U openmentor -q; do sleep 1; done
docker cp /tmp/restore.dump pg-drill:/tmp/drill.dump
docker exec pg-drill pg_restore -U openmentor -d openmentor /tmp/drill.dump

# 4. Sanity queries: row counts and recency
docker exec pg-drill psql -U openmentor -c "SELECT count(*) FROM mentors;"
docker exec pg-drill psql -U openmentor -c "SELECT count(*) FROM tags;"
docker exec pg-drill psql -U openmentor -c "SELECT * FROM schema_migrations;"
docker exec pg-drill psql -U openmentor -c \
    "SELECT max(created_at) FROM client_requests;"   # should be ~last 24h

# 5. Clean up — all three copies hold production personal data
docker rm -f pg-drill
rm -f /tmp/restore.dump
docker exec openmentor-postgres-backup rm -f /backups/restore-candidate.dump
```

**Rehearsed 2026-08-04** (audit H11) against a scratch cluster built from
`api/migrations/*.up.sql` rather than a production dump, since the procedure —
not the data — was what needed proving: `pg_dump -Fc` inside the container →
`docker cp` out → `pg_restore` into a throwaway → identical table count (9),
row counts and `schema_migrations` version (9, clean) on both sides;
`pg_restore --clean --if-exists` into the non-empty database (the variant noted
below) also round-tripped. The `docker cp` mode hazard reproduced exactly as
described above: 0644 on the host under `umask 077`, 600 only after the explicit
`chmod`. Both containers removed afterwards. The **remaining** unproven step is
the one that needs production credentials: that a real object in the backup
bucket restores. Do that at the next drill and record it here.

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
| Sidecar logs `FAILURE ... error=s3_upload_failed` (dump kept locally) | Bad/wrong `BACKUP_AWS_*` creds (there is **no** fallback to `S3_STORAGE_*` — SECURITY M12), wrong region, or bucket policy | Fix creds/bucket; the dump is still in the `openmentor-postgres-backups` volume — upload it manually with the `docker exec openmentor-postgres-backup sh -c '…aws s3 cp…'` form from "How to reach S3" above (there is no `aws` on the VM itself) |
| Sidecar container is `restarting` and logs `FATAL: BACKUP_S3_BUCKET is set but BACKUP_AWS_ACCESS_KEY_ID / ... are not` | `BACKUP_S3_BUCKET` set with empty dedicated creds. Under `restart: always` this loops forever, and `deploy-remote.sh` reads a `restarting` container as unhealthy → **auto-rollback of a healthy deploy** | Fill `BACKUP_AWS_ACCESS_KEY_ID`/`BACKUP_AWS_SECRET_ACCESS_KEY` in `.env.production`, or clear `BACKUP_S3_BUCKET` to accept local-only backups; then redeploy |
| Sidecar logs the loud `BACKUP_S3_BUCKET is not set` warning in production | Off-site backups not configured | Set `BACKUP_S3_BUCKET` **and** the dedicated `BACKUP_AWS_*` creds in `.env.production`, then redeploy |
| `pg_restore: error: unsupported version` | Dump made by a newer pg_dump than the restoring server | Restore into the same or newer major (`postgres:16.14-alpine` or later) |

## Notes

- Backups contain personal data: the S3 backup bucket must be private, encrypted (SSE-S3 is fine) and in the EU region; deletion requests age out of dumps with `BACKUP_RETENTION_DAYS` — this is stated in the privacy policy (see `data-deletion.md`).
- The `postgres` container publishes no ports; all admin access is `docker exec -it openmentor-postgres psql -U openmentor` on the VM.
- Config source of truth: `infra/docker-compose.yml`, `infra/postgres-backup/backup.sh`, and the `BACKUP_*`/`POSTGRES_*` sections of the env templates.
