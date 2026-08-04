# Runbook: PostgreSQL 16 → 18 major upgrade

**Trigger:** Dependabot #4276-style image bump (`postgres:16.14-alpine` → `postgres:18.4-alpine`, PR #7). **That PR must not be merged on its own.** A major Postgres upgrade is planned maintenance with a write outage, not a dependency bump — this runbook is the work the bump implies.

Production Postgres runs as the `postgres` container on the Hetzner VM (DECISIONS D2), defined in `infra/docker-compose.yml`, with data in the **external** volume `openmentor-postgres-data`.

Read `postgres-backup-restore.md` first — step 1 below depends on it, and path (B) here *is* dump/restore.

## Why the plain image bump breaks

Three independent problems, in the order they would bite you.

### 1. The data directory moved in the PG18 image (the dangerous one)

The official image changed `PGDATA` in 18+ to match the `pg_ctlcluster` layout:

| | PG16 image | PG18 image |
|---|---|---|
| `PGDATA` | `/var/lib/postgresql/data` | `/var/lib/postgresql/18/docker` |
| `VOLUME` | `/var/lib/postgresql/data` | `/var/lib/postgresql` |

Compose currently mounts `postgres-data:/var/lib/postgresql/data`. Under the PG18 image that path is no longer `PGDATA` — it is just some directory that happens to have a volume on it. The entrypoint would find `/var/lib/postgresql/18/docker` **empty**, run `initdb`, and come up as a brand-new empty cluster. `migrate` would then happily create the schema and the site would serve an empty catalog.

This is worse than a crash: nothing errors. The real data is still sitting untouched in the volume, but the site is live and empty, and every write from that moment is going into the new cluster. **Any upgrade path must pin `PGDATA` or move the mount explicitly** (both paths below do).

### 2. PG18 turns on data checksums by default

`initdb` flipped its default between these versions:

- PG16 `src/bin/initdb/initdb.c`: `static bool data_checksums = false;`
- PG18 `src/bin/initdb/initdb.c`: `static bool data_checksums = true;`

`pg_upgrade` refuses to run when the two clusters disagree (`old and new cluster pg_controldata checksum versions do not match`). The existing production cluster was initialized by the PG16 image, so it has checksums **off**. Path (A) therefore initializes the new cluster with `--no-data-checksums`. (Enabling checksums on the old cluster instead with `pg_checksums --enable` is possible but needs the cluster offline and rewrites every page — not worth it during a maintenance window. Do it later as its own task if you want checksums.)

### 3. Everything else pinned to 16

These move in the **same commit** as the compose bump — a `pg_dump` older than the server refuses to run, so the backup sidecar breaks the first night after the upgrade if it is left behind:

| File | Current | Notes |
|---|---|---|
| `infra/docker-compose.yml` | `postgres:16.14-alpine` | + the `PGDATA`/mount change from §1 |
| `infra/postgres-backup/Dockerfile` | `FROM postgres:16.14-alpine` | **must** be ≥ server major, else nightly dumps fail |
| `.github/workflows/ci-api.yml` | `postgres:16-alpine` | smoke test |
| `README.md`, `api/README.md` | `postgres:16-alpine` | local dev |
| `docs/runbooks/postgres-backup-restore.md` | `postgres:16.14-alpine` | drill instructions + the "unsupported version" row |
| `docs/runbooks/database-observability.md` | "compose runs `postgres:16.14-alpine`" | |

Also: `shared_preload_libraries=pg_stat_statements` is required by Grafana Cloud Database Observability (`database-observability.md`). The extension's SQL objects do **not** upgrade themselves — see step 7.

## Pick a path

| | (A) `pg_upgrade --link` | (B) dump + restore |
|---|---|---|
| Write outage | ~1–2 min | size-dependent; minutes at current scale |
| Rollback | keep the old volume, repoint compose | keep the old volume, repoint compose |
| Risk | more moving parts, but no logical re-import | simplest to reason about; rebuilds everything |
| Recommended | large DB | **yes — use this at current scale** |

At the current catalog size the dump is small and (B)'s outage is a few minutes, so take (B) unless the dump has grown to where the restore time stops being acceptable. (B) is also the path you already rehearse every quarter in the restore drill, which is the strongest argument for it.

Both paths keep the old volume untouched, so rollback is always "point compose back at the old volume and start the 16 image".

---

## Before the window (no outage, do this days ahead)

1. **Run a restore drill** on the *current* dump into a **PG18** throwaway, not a PG16 one. This is the real go/no-go: it proves the dump restores under 18 before you touch production.

   Runs **on the VM**: the backup bucket's credentials exist only inside the
   `openmentor-postgres-backup` container, never on the VM itself — see
   `postgres-backup-restore.md` § "How to reach S3", whose two fetch blocks are
   step 1 here. The throwaway gets no `--network`, so it cannot reach production.

   ```bash
   # /tmp/restore.dump at mode 600, per postgres-backup-restore.md
   docker run -d --name pg18-drill --network none -e POSTGRES_USER=openmentor \
       -e POSTGRES_PASSWORD="$(openssl rand -hex 16)" \
       -e POSTGRES_DB=openmentor postgres:18.4-alpine
   until docker exec pg18-drill pg_isready -U openmentor -q; do sleep 1; done
   docker cp /tmp/restore.dump pg18-drill:/tmp/drill.dump
   # pg_restore from the 18 image; a 16-produced custom dump restores into 18 fine
   docker exec pg18-drill pg_restore -U openmentor -d openmentor -v /tmp/drill.dump 2>&1 | tail -30
   docker exec pg18-drill psql -U openmentor -c "SELECT count(*) FROM mentors;"
   docker exec pg18-drill psql -U openmentor -c "SELECT max(created_at) FROM client_requests;"
   docker rm -f pg18-drill
   rm -f /tmp/restore.dump
   docker exec openmentor-postgres-backup rm -f /backups/restore-candidate.dump
   ```

   Investigate every `pg_restore` warning. Expect noise about the `postgres` role/ownership; do **not** wave through anything mentioning a type, extension or function.

2. **Prepare the branch** with all the version pins from §3 plus the compose changes below, and let CI go green.

3. **Announce the window.** Mentor/mentee writes fail during it — the API returns 5xx once Postgres stops.

---

## Path (B): dump + restore — recommended

On the VM, in `/opt/openmentor/infra`:

```bash
# 1. Stop writers. Keep traefik up so LE certs don't churn.
docker compose stop backend worker migrate postgres-backup

# 2. Final dump from the RUNNING 16 server, with the 16 sidecar
docker exec openmentor-postgres-backup backup.sh once
docker logs openmentor-postgres-backup --tail 5     # expect SUCCESS
# Then fetch it with the two blocks in postgres-backup-restore.md
# § "Fetching a dump out of the sidecar" — the VM has no aws CLI and no AWS
# credentials; only the sidecar does. That leaves /tmp/restore.dump at mode 600.
cp -p /tmp/restore.dump /tmp/upgrade.dump   # keeps 600; the name this runbook uses below

# 3. Stop postgres and KEEP the old volume as the rollback point
docker compose stop postgres && docker compose rm -f postgres
docker volume create openmentor-postgres-data-pg16
docker run --rm -v openmentor-postgres-data:/from -v openmentor-postgres-data-pg16:/to \
    alpine sh -c "cp -a /from/. /to/"

# 4. Fresh empty volume for 18
docker volume rm openmentor-postgres-data
docker volume create openmentor-postgres-data

# 5. Ship the branch, then start postgres alone.
#    The VM has NO monorepo checkout — only /opt/openmentor/infra, rsynced by
#    the `infra` deploy target. So this step runs on your WORKSTATION, from the
#    branch, and it is `deploy.sh` that carries the new compose file over:
#
#      cd infra && ./deploy.sh all        # (from the workstation, on the branch)
#
#    `deploy.sh all` also brings postgres up as part of the converge. If you want
#    postgres alone first, use `./deploy.sh infra` to sync the compose file and
#    then, back on the VM:
docker compose up -d postgres     # wait for (healthy)
docker compose ps postgres

# 6. Restore
docker cp /tmp/upgrade.dump openmentor-postgres:/tmp/upgrade.dump
docker exec openmentor-postgres \
    pg_restore -U openmentor -d openmentor -v /tmp/upgrade.dump 2>&1 | tail -40
```

Then continue at **After either path** below.

## Path (A): `pg_upgrade --link` — for when the DB outgrows (B)

Sketch, not a script — rehearse it on a VM snapshot first. `--link` hard-links the data files, so it is fast but **destroys the old cluster's usability on failure**; the copy in step 3 above is what makes it survivable.

```bash
# with both binaries available; run initdb for the NEW cluster explicitly:
initdb -D /var/lib/postgresql/18/docker --no-data-checksums   # see §2
pg_upgrade \
  --old-datadir=/var/lib/postgresql/data \
  --new-datadir=/var/lib/postgresql/18/docker \
  --old-bindir=/usr/lib/postgresql/16/bin \
  --new-bindir=/usr/lib/postgresql/18/bin \
  --link --check          # drop --check for the real run
```

`--check` is a dry run and safe to do ahead of the window. Note the alpine images do not carry two majors side by side, so this needs either a purpose-built image containing both, or the `tianon/postgres-upgrade` approach. That packaging work is the main reason (B) is recommended at current scale.

---

## After either path

7. **Update `pg_stat_statements`.** The library loads from `shared_preload_libraries`, but the SQL objects are versioned separately and carry over from 16 — Grafana's Database Observability queries the view, so a stale extension shows up as missing/renamed columns:

   ```bash
   docker exec openmentor-postgres psql -U openmentor -d openmentor \
       -c "ALTER EXTENSION pg_stat_statements UPDATE;"
   docker exec openmentor-postgres psql -U openmentor -d openmentor \
       -c "SELECT extname, extversion FROM pg_extension;"
   ```

   On path (B) the extension comes from the dump, so run this there too.

8. **`ANALYZE` the whole database.** Neither path carries planner statistics across a major upgrade, and without this the first traffic hits sequential scans on cold stats:

   ```bash
   docker exec openmentor-postgres vacuumdb -U openmentor -d openmentor --analyze-in-stages
   ```

9. **Verify, then release traffic:**

   ```bash
   docker exec openmentor-postgres psql -U openmentor -c "SELECT version();"
   docker exec openmentor-postgres psql -U openmentor -c "SELECT count(*) FROM mentors;"
   docker exec openmentor-postgres psql -U openmentor -c "SELECT max(created_at) FROM client_requests;"
   # schema_migrations must match the repo's latest migration
   docker exec openmentor-postgres psql -U openmentor -c "SELECT * FROM schema_migrations;"

   docker compose up -d migrate      # must be a no-op: "no change"
   docker compose up -d backend worker postgres-backup
   curl -fsS https://openmentor.io/api/healthcheck
   ```

10. **Force a dump on the new sidecar the same day** — do not wait for 03:30 to find out the 18 image never got built:

    ```bash
    docker exec openmentor-postgres-backup backup.sh once
    docker logs openmentor-postgres-backup --tail 5
    ```

11. **Watch for 24 h**: Grafana Database Observability (pg_stat_statements flowing), API error rate, and query latency. Then, and only then:

    ```bash
    docker volume rm openmentor-postgres-data-pg16
    ```

## Rollback

Valid until step 11 deletes the old volume. Writes taken on 18 after cutover are lost — that is the cost, and why the window is announced.

```bash
docker compose stop backend worker migrate postgres-backup postgres
docker compose rm -f postgres
docker volume rm openmentor-postgres-data
docker volume create openmentor-postgres-data
docker run --rm -v openmentor-postgres-data-pg16:/from -v openmentor-postgres-data:/to \
    alpine sh -c "cp -a /from/. /to/"

# Back to the 16 pins. There is no git checkout on the VM to revert — the 16
# compose file has to be shipped from a workstation, from the pre-upgrade commit:
#
#   git checkout <previous-sha> && cd infra && ./deploy.sh all
#
# `rollback.sh` alone is NOT enough here: it only moves image tags, and the
# postgres image pin and PGDATA live in docker-compose.yml, which only the
# `infra` target syncs. It will also refuse if the 18 window applied a migration
# the older backend image does not carry (see infra/DEPLOYMENT.md, "Rolling back
# across a migration boundary").
docker compose up -d postgres && docker compose up -d backend worker postgres-backup
```

## Compose changes this requires

Beyond the image tag, `PGDATA` must be pinned so the mount and the data directory cannot drift apart again (§1):

```yaml
  postgres:
    image: postgres:18.4-alpine
    environment:
      # PG18 moved PGDATA to /var/lib/postgresql/18/docker and the image VOLUME
      # to /var/lib/postgresql. Pin it back to the path the external volume is
      # mounted on, otherwise the entrypoint initdb's a FRESH empty cluster
      # and the real data is silently ignored.
      - PGDATA=/var/lib/postgresql/data
    volumes:
      - postgres-data:/var/lib/postgresql/data
```

Keeping the mount where it is (rather than moving it to `/var/lib/postgresql`) means the external volume, the deploy scripts and this runbook's rollback all keep working unchanged.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| Site up, catalog empty, no errors | `PGDATA` not pinned — 18 initdb'd a fresh cluster (§1) | Stop everything, do not let more writes land, restore per Rollback, then add `PGDATA` |
| `database files are incompatible with server` | 18 server pointed at a 16 data directory | Expected without an upgrade — follow path (A) or (B) |
| `old and new cluster pg_controldata checksum versions do not match` | §2 | `initdb --no-data-checksums` for the new cluster |
| Nightly backup fails after upgrade | sidecar still `FROM postgres:16.14-alpine`; `pg_dump` < server | Rebuild sidecar on 18 (§3) |
| Grafana DB Observability panels empty | `pg_stat_statements` extension not updated | Step 7 |
| First-hour latency spike | no planner stats after major upgrade | Step 8 |
