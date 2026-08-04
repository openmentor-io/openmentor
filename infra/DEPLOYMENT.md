# Production Deployment Guide

This guide explains how to deploy OpenMentor to the production VM from your
local machine using `./deploy.sh`.

## Prerequisites

1. **Docker** installed and running locally
2. **SSH access** to the production VM (Hetzner Cloud, see DECISIONS D2)
3. **Container registry credentials** — AWS ECR (DECISIONS D19):
   - **aws CLI** installed locally with an identity that has ECR push access
     (`aws sts get-caller-identity` must work — profile, SSO, or env creds)
   - `ECR_REGISTRY` (`<account-id>.dkr.ecr.eu-central-1.amazonaws.com`) and
     `AWS_REGION` in `.env.production`
   - no VM-side AWS credentials needed (short-lived ECR tokens are piped in)
     IAM user's pull-only keys) in `.env.production` — the VM logs in with
     them before pulling
4. **Full monorepo checkout** — the frontend (`../web`) and backend
   (`../api`) are sibling directories of this `infra/` directory; check out
   the commit you want to ship — the image tag is the repo's short commit SHA
5. **All changes committed** (tags come from `git rev-parse`)

## Initial Setup

### 1. Create the deployment configuration file

One file drives everything:

```bash
cp .env.production.example .env.production
```

`.env.production` contains three kinds of values (see the template's
sections):

- **Deployment machine settings** — used by `deploy.sh`/`rollback.sh`:
  `ECR_REGISTRY`, `AWS_REGION`, `VM_SSH_HOST`,
  `VM_SSH_USER`, `VM_SSH_KEY_FILE`
- **Build-time variables** — `NEXT_PUBLIC_*` (and Faro/PostHog sourcemap
  vars), baked into the frontend image during `docker build`
- **Runtime secrets** — everything the containers read from
  `/opt/openmentor/infra/.env` on the VM (`DATABASE_URL`, `S3_STORAGE_*`,
  `SES_*`, `WORKER_AUTH_TOKEN`, trigger URLs, `JWT_SECRET`,
  Grafana Cloud credentials, ...)

**Security**: never commit `.env.production` (it is gitignored).

### 2. Verify SSH access

```bash
ssh -i /path/to/ssh-key <user>@<vm-ip>
```

The VM must have Docker + docker-compose and the monorepo checked out at
`/opt/openmentor` (compose files live in `/opt/openmentor/infra`).

Note: when migrating a VM from the pre-monorepo `/opt/openmentor-infra`
checkout, stop the old stack first
(`cd /opt/openmentor-infra && docker-compose down`) — the compose project
name changes with the move, and the pinned container names would otherwise
conflict on the first deploy from `/opt/openmentor/infra`.

## Deploying to Production

```bash
./deploy.sh [targets...] [options]

# Targets (default: frontend backend):
./deploy.sh                        # frontend + backend
./deploy.sh frontend               # only the frontend container
./deploy.sh backend                # backend + worker + migrate (one image)
./deploy.sh infra                  # sync infra/ config, converge compose changes
./deploy.sh all                    # frontend backend infra

# Options:
#   --tag TAG     use TAG instead of the git commit SHA
#   --yes, -y     skip the confirmation prompt
#   --dry-run     print the deployment plan and exit
#   --staging     deploy to the staging VM (VM_SSH_*_STAGING vars)
```

The script will:

1. Validate credentials and config, print the plan, confirm (unless `--yes`)
2. Build the targeted Docker images locally, tagged with the monorepo's
   short commit SHA (`DOCKER_TAG_POLICY.md`)
3. Fetch the currently deployed tags from the VM for services **not** being
   deployed (they keep their tags)
4. Push the built images to the registry
5. `infra` target only: rsync `infra/` to `/opt/openmentor/infra`
   (excluding `.env*`, `logs/`, `alloy-secrets/`; no `--delete`) with
   `--checksum --itemize-changes` to learn which files changed
6. Upload `.env.production` to the VM as `/opt/openmentor/infra/.env`
   (mode 600) with `FRONTEND_IMAGE_TAG`/`BACKEND_IMAGE_TAG` appended
7. Write the Alloy DB-observability secret (`POSTGRES_OBS_DSN`) to
   `alloy-secrets/` on the VM
8. Run `docker-compose pull && docker-compose up -d` on the VM
   (`--remove-orphans` when `infra` is targeted). Compose recreates **only**
   the services whose image tag or definition changed. Bind-mounted config
   is handled explicitly: a changed `alloy/config.alloy` triggers
   `docker-compose restart alloy`, a changed `postgres-backup/` triggers a
   sidecar image rebuild (compose alone would miss both — see the
   bind-mount inventory in `README.md`)
9. Health-check the apps inside their containers:
   - frontend `http://localhost:3000/api/healthcheck`
   - backend `http://localhost:8081/api/healthcheck`
   - worker `http://localhost:8090/healthz`
   - postgres `pg_isready` + the backup sidecar's compose healthcheck
     (`.State.Health.Status` — docker keeps an *unhealthy* container in state
     `running`, so `.State.Status` alone made stale dumps invisible here;
     `starting` and a VM whose compose file predates the healthcheck pass)
10. **Automatically roll back** (restore the previous `.env`, re-pull,
    re-up) if any health check fails — except a backup sidecar that is running
    but `unhealthy`, which ends the deploy with **exit 2** and no rollback:
    reverting working images cannot make a `pg_dump` run, and a deploy that
    aborts halfway is worse than the stale dump
11. Verify the public endpoint `https://$DOMAIN/api/healthcheck`

Notes:

- Steps 8–10 (volume, sidecar rebuild, pull, converge, network-attachment
  guard, health checks, auto-rollback) are `deploy-remote.sh` — the single
  canonical remote script. `deploy.sh` and the CI workflow
  (`../.github/workflows/deploy.yml`) both pipe the **local checked-out
  copy** over ssh stdin, so the remote logic is edited once and consumed by
  both paths, and never depends on the rsynced copy on the VM being fresh.
- The `migrate` service runs database migrations before backend and worker
  start (`depends_on: service_completed_successfully`) — this holds on every
  `up -d`.
- Postgres image pin bumps recreate the container **safely**: the data lives
  in the external `openmentor-postgres-data` volume. Minor/patch versions
  only — major upgrades follow `../docs/runbooks/postgres-backup-restore.md`.

### First deploy after the per-service env allowlists (P10)

Recommended order: **`./deploy.sh infra` (or `all`) first**, then normal
app-only deploys.

The per-service `environment:` allowlists replaced `env_file: .env.runtime`.
That change lives in `docker-compose.yml`, which only the `infra` target
syncs, while `deploy-remote.sh` is always piped from the local checkout — so
on a default `./deploy.sh` the VM would run the *old* compose file against the
*new* remote script. Compose treats `env_file` as required, so a VM without
`.env.runtime` cannot even be inspected:

```
env file /opt/openmentor/infra/.env.runtime not found
```

`deploy-remote.sh` and `rollback.sh` therefore decide from the compose file
**on the VM**: while it still declares `env_file: .env.runtime` they
regenerate that file (mode 600, image-tag lines stripped) and print the
upgrade order; the first deploy that carries the new compose file is the one
that deletes it. An app-only deploy is safe either way — it just leaves the
shared secret file in place until `infra` is synced. Covered by
`make deploy-tests`.

## Rollback

```bash
./rollback.sh <commit-sha>                 # roll BOTH images to <sha>
./rollback.sh --frontend <sha>             # frontend only
./rollback.sh --backend <sha>              # backend/worker/migrate only
./rollback.sh                              # prompt for a tag interactively
```

Reads the same `.env.production`, SSHes to the VM, updates
`FRONTEND_IMAGE_TAG`/`BACKEND_IMAGE_TAG` in `/opt/openmentor/infra/.env`,
pulls, re-converges, and verifies the same health checks as `deploy.sh`. It takes
the same `.deploy.lock` a deploy takes, so a rollback and a deploy cannot
converge the VM at the same time.

### The three `.env` files on the VM (H9)

| File | Written by | Meaning |
|---|---|---|
| `.env` | every deploy/rollback | what compose reads right now |
| `.env.lastgood` | `deploy-remote.sh`, `rollback.sh` — **only after every application health check passed** | the auto-rollback target. A tag that was merely attempted can never appear here |
| `.env.backup.<epoch>` | every writer, before it changes `.env` | history/forensics, 5 newest kept |

Manual recovery on the VM is therefore `cp .env.lastgood .env && docker compose up -d`.
The pre-H9 single `.env.backup` slot is gone: two overlapping deploys overwrote
it for each other, so a failed deploy B could "roll back" to deploy A's
unverified tags.

### Rollback does NOT revert database migrations

Neither `rollback.sh` nor `deploy-remote.sh`'s automatic health-check rollback
runs down-migrations — they only move image tags. Migrations are designed to be
backward-compatible with the previous release, so this is normally fine.

The exception is a release that **renames or removes** existing data, where the
older image expects the old shape. `000009_modernise_tags` is the current
example: rolling back past it leaves a pre-D30 frontend offering tag names the
database no longer has, so its catalog category filters match nothing. (Profile
saves are safe — the API rejects a save whose tags all fail to resolve rather
than wiping them.)

If you must roll back past such a release, apply the down-migration by hand.
Two constraints make this manual:

- `/app/migrate` in the backend image is a small custom runner that only
  migrates **up** — it takes no `down` argument, and the image does not ship
  the `golang-migrate` CLI.
- Migrations are baked into the image at `/app/migrations`, so once you restore
  the older tag, the newer `*.down.sql` is no longer on the box. Get it from
  git (or run this **before** retagging).

Apply the SQL with the `psql` already present in the postgres container, then
correct the version golang-migrate tracks — it will not notice the change on
its own:

```bash
cd /opt/openmentor/infra

# 1. Apply the down migration (ON_ERROR_STOP so a failure doesn't half-apply)
docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -v ON_ERROR_STOP=1 \
  < api/migrations/000009_modernise_tags.down.sql

# 2. Point schema_migrations at the previous version (here: 9 -> 8)
docker compose exec -T postgres \
  psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "UPDATE schema_migrations SET version = 8, dirty = false;"
```

Verified to round-trip: the next normal deploy re-applies `000009` and returns
the database to version 9 with the full taxonomy.

Check the migration's own `.down.sql` header first — some are explicitly lossy
(`000009` restores the tag rows and names, but cannot restore mentor
associations for the tags it deleted).

Manual fallback on the VM:

```bash
cd /opt/openmentor/infra
sed -i 's/^BACKEND_IMAGE_TAG=.*/BACKEND_IMAGE_TAG=<previous-working-sha>/' .env   # and/or FRONTEND_IMAGE_TAG
docker-compose pull && docker-compose up -d
```

## Monitoring a Deployment

1. **Immediately**: `curl https://openmentor.io/api/healthcheck`
2. **Logs** (on the VM): `docker-compose logs -f backend worker frontend`
3. **Grafana Cloud**: request/error rates, latency, worker job outcomes, Loki
   logs

## Troubleshooting

### Build fails

```bash
cd ../web && docker build .    # see the full frontend error
cd ../api && docker build .    # see the full backend error
```

### Push fails

```bash
# Test the ECR login manually (uses your local aws CLI identity)
aws ecr get-login-password --region eu-central-1 \
  | docker login --username AWS --password-stdin <account-id>.dkr.ecr.eu-central-1.amazonaws.com
```

### Health checks fail after deploy

```bash
ssh <vm>
cd /opt/openmentor/infra
docker-compose logs frontend backend worker
docker exec openmentor-backend curl -s http://localhost:8081/api/healthcheck
docker exec openmentor-worker curl -s http://localhost:8090/healthz
```

The deploy script restores the previous `.env` automatically; fix the issue
and redeploy. Common causes: missing/renamed env vars (compare against
`.env.production.example`), failed migration (check
`docker logs openmentor-migrate`), DB unreachable.

## Best Practices

- Test locally first: `./deploy-dev.sh` (same CLI/flow against the local
  dev stack)
- Deploy from a clean, committed tree
- Note the deployed SHAs (printed in the summary) for quick rollback
- Watch Grafana for 5–10 minutes after deploying

## Related Documentation

- [README.md](README.md) — architecture and stack overview
- [ENVIRONMENT_VARIABLES.md](ENVIRONMENT_VARIABLES.md) — env file layering
- [DOCKER_TAG_POLICY.md](DOCKER_TAG_POLICY.md) — image tagging strategy
- [../.github/workflows/deploy.yml](../.github/workflows/deploy.yml) — CI deploy
  (manual `workflow_dispatch`)
