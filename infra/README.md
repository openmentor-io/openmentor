# OpenMentor Infrastructure

Docker Compose infrastructure for [openmentor.io](https://openmentor.io): a
Next.js frontend, a Go API, a Go background worker, and a Grafana Cloud
observability pipeline — all running on a single Hetzner Cloud VM.

## Table of Contents

- [Architecture Overview](#architecture-overview)
- [Services](#services)
- [Repository Layout](#repository-layout)
- [Quick Start (Local Development)](#quick-start-local-development)
- [Environment Variables](#environment-variables)
- [Production Deployment](#production-deployment)
- [CI/CD Pipeline](#cicd-pipeline)
- [Monitoring & Observability](#monitoring--observability)
- [Backups](#backups)
- [Known TODOs](#known-todos)
- [Security Considerations](#security-considerations)

---

## Architecture Overview

One **active** provider (Hetzner), passive AWS/Cloudflare accounts
(see `../docs/migration/05-infrastructure.md` and `DECISIONS.md` D1/D2/D6/D15):

```
Hetzner VM (only thing operated day-to-day)
└── docker compose: traefik / frontend / backend(api) / worker / migrate
                    / postgres / postgres-backup / alloy / cadvisor
AWS (passive, one account):  S3 = profile images (D15) · SES = email (D1)
Cloudflare (passive):        DNS
cr.yandex → ghcr.io:         container images (registry swap pending, P6.4)
Grafana Cloud (free tier):   metrics / logs / traces / profiles
```

Request flow:

```
Internet (HTTPS)
   │
Traefik :80/:443  ── Let's Encrypt (Cloudflare DNS-01), HTTP→HTTPS redirect
   │
   ├── ${DOMAIN}, www.${DOMAIN}  → frontend :3000 (Next.js)
   └── mcp.${DOMAIN} (POST)      → backend :8081 /api/internal/mcp
                                        │
frontend ── internal Docker network ──> backend (Go API)
                                        │            │
                                        │   fire-and-forget triggers
                                        ▼            ▼
                                   PostgreSQL     worker :8090 (Go)
                                                  /jobs/* + daily crons

alloy :12345 scrapes frontend/backend/worker/cadvisor and tails their logs,
shipping everything to Grafana Cloud.
```

Scale seams: `DATABASE_URL` → managed PG; VM resize; the worker container
scales/deploys independently of the API; S3/SES scale on their own.

## Services

| Service | Image | Exposure | Purpose |
|---|---|---|---|
| `traefik` | traefik:v2.10 | :80/:443 public | TLS termination (Let's Encrypt via Cloudflare DNS-01), routing |
| `frontend` | openmentor-frontend | via Traefik | Next.js web app |
| `backend` | openmentor-backend | internal (+ `mcp.${DOMAIN}` POST via Traefik) | Go REST API (`/app/main`) |
| `worker` | openmentor-backend (same image, `/app/worker`) | internal :8090 | Async event triggers from the API (`/jobs/*`, `X-Worker-Token` auth) + daily cron jobs. Replaces the deprecated `openmentor-func` Azure Functions app (D6) |
| `migrate` | openmentor-backend (`/app/migrate`) | — | Runs DB migrations once before backend/worker start |
| `postgres` | postgres:16.14-alpine | internal only (no published ports) | Production database (DECISIONS D2). Data in the **external** volume `openmentor-postgres-data` (survives `compose down -v`; created by deploy scripts). Admin access via `docker exec -it openmentor-postgres psql`. Dev overlay overrides it with dev creds + host :5433 |
| `postgres-backup` | built from `postgres-backup/` | internal only | Nightly `pg_dump -Fc` of the database at `BACKUP_TIME` (UTC) → S3 (`BACKUP_S3_BUCKET`) with `BACKUP_RETENTION_DAYS` pruning; local `openmentor-postgres-backups` volume fallback. Disabled in the dev overlay |
| `alloy` | grafana/alloy | internal :12345 | Metrics scraping, log tailing, OTLP traces, Pyroscope profiles → Grafana Cloud |
| `cadvisor` | cadvisor | internal | Container resource metrics |

## Repository Layout

```
openmentor-infra/
├── docker-compose.yml          # Production stack
├── docker-compose.dev.yml      # Dev overlay (local builds, dev postgres creds, no traefik/alloy/cadvisor/backups)
├── postgres-backup/            # Backup sidecar image (pg_dump → S3, see Backups)
├── .env.example                # Local development env template
├── .env.production.example     # Production env template (deploy creds + build args + runtime secrets)
├── deploy.sh                   # Build + push + deploy to the VM (health checks + auto-rollback)
├── deploy-dev.sh               # Full local build + stack bring-up
├── dev.sh                      # Day-to-day local stack helper (up/down/logs/health/db)
├── rollback.sh                 # Roll production back to a previous image tag
├── alloy/config.alloy          # Grafana Alloy pipeline
├── grafana/                    # Dashboards & alerts as code (jsonnet → dist/)
├── posthog/dashboards/         # Product analytics dashboards as code
├── migration/                  # One-off Yandex Object Storage → AWS S3 image copy (D15)
├── DEPLOYMENT.md               # Production deployment guide
├── ENVIRONMENT_VARIABLES.md    # Env file layering explained
├── DOCKER_TAG_POLICY.md        # Why images are tagged with commit SHAs
└── docs/troubleshooting.md     # Operational troubleshooting
```

The sibling repos `../openmentor` (frontend) and `../openmentor-api` (Go API +
worker + migrations) must be cloned next to this repo for local builds.

## Quick Start (Local Development)

### Prerequisites

- Docker 20.10+ with Compose 2.x
- Sibling repos cloned:

```
~/projects/
├── openmentor/        # frontend
├── openmentor-api/    # backend + worker
└── openmentor-infra/  # this repo
```

### 1. Configure environment

```bash
cd openmentor-infra
cp .env.example .env
# fill in real values (tokens, S3/SES creds, PostHog keys, ...)
```

### 2. Start the stack

```bash
./dev.sh up-d          # build + start detached
# or: ./deploy-dev.sh  # full rebuild with health-check verification
# or raw compose:
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d --build
```

The dev overlay:
- builds frontend/backend images from the sibling repos instead of pulling,
- overrides the `postgres` service with dev credentials, a disposable
  `openmentor-postgres-data-dev` volume and host port **5433** (only one
  database runs in the merged stack),
- disables `traefik`, `alloy`, `cadvisor` and `postgres-backup`
  (via `profiles: [production-only]`) so nothing pollutes production
  Grafana Cloud data or backup buckets.

### 3. Access services

- Frontend: http://localhost:3000
- Backend health: http://localhost:8081/api/healthcheck
- Worker health: http://localhost:8090/healthz
- Postgres: `psql postgresql://openmentor:password@localhost:5433/openmentor`

### 4. Stop

```bash
./dev.sh down          # stop
./dev.sh clean         # stop + remove volumes
```

## Environment Variables

See `ENVIRONMENT_VARIABLES.md` for how the env files layer, and the two
committed templates for the full annotated variable list:

- `.env.example` → `.env` (local development, read by compose `env_file`)
- `.env.production.example` → `.env.production` (deployment credentials,
  frontend build args, and runtime secrets; `deploy.sh` uploads it to
  `/opt/openmentor-infra/.env` on the VM)

Highlights:

- `DATABASE_URL` — Postgres for migrate/backend/worker
- `S3_STORAGE_*` / `NEXT_PUBLIC_S3_STORAGE_*` — AWS S3 profile images (D15)
- `SES_*`, `MODERATORS_EMAIL`, `DEV_EMAIL_OVERRIDE` — AWS SES email via the worker (D1)
- `WORKER_AUTH_TOKEN`, `WORKER_CRON_ENABLED`, `*_TRIGGER_URL` — API→worker wiring
- `JWT_SECRET`, `MCP_AUTH_TOKEN`, `INTERNAL_MENTORS_API`/`GO_API_INTERNAL_TOKEN` — auth
- `GCLOUD_*`, `O11Y_*` — Grafana Cloud observability
- `ANALYTICS_PROVIDER`, `POSTHOG_*`, `NEXT_PUBLIC_POSTHOG_*` — product analytics

Generate secrets with `openssl rand -base64 32` (or `-hex 32` for the worker token).

## Production Deployment

Production is a single Hetzner Cloud VM (DECISIONS D2) with Docker, the repo
checked out at `/opt/openmentor-infra`, and firewall open on 22/80/443.

### Deploy from a workstation

```bash
cp .env.production.example .env.production   # once; fill in everything
./deploy.sh                    # build + push + deploy frontend and backend/worker
./deploy.sh --frontend-only    # or --backend-only / --skip-frontend / --skip-backend
```

`deploy.sh`:

1. builds images tagged with the source repos' short commit SHAs
   (see `DOCKER_TAG_POLICY.md` — never `latest`),
2. pushes them to the container registry (currently `cr.yandex`; ghcr.io swap
   tracked as **P6.4**),
3. uploads `.env.production` to the VM as `/opt/openmentor-infra/.env`
   (mode 600) and writes the Alloy DB-observability secret,
4. ensures the external `openmentor-postgres-data` volume exists
   (idempotent `docker volume create`), then runs
   `docker-compose pull && up -d` on the VM,
5. health-checks frontend (`/api/healthcheck`), backend (`/api/healthcheck`),
   worker (`/healthz`) and postgres (`pg_isready`) **inside** the containers
   plus the backup sidecar's running state, and
6. **automatically rolls back** to the previous `.env` (previous image tags)
   if any health check fails, then verifies the rollback.

### Rollback manually

```bash
./rollback.sh <previous-commit-sha>
```

See `DEPLOYMENT.md` for the full guide and troubleshooting.

## CI/CD Pipeline

`.github/workflows/deploy.yml` builds/pushes both images and deploys over SSH
with the same health-check + rollback logic. It is currently
**manual-trigger only** (`workflow_dispatch`); the push trigger is commented
out. Required repo secrets: `YANDEX_REGISTRY_ID`, `YANDEX_SA_KEY` (until
P6.4), `VM_SSH_HOST`, `VM_SSH_USER`, `VM_SSH_KEY`, `DOMAIN`,
`NEXT_PUBLIC_S3_STORAGE_ENDPOINT`, `NEXT_PUBLIC_S3_STORAGE_BUCKET`,
`RECAPTCHA_V2_SITE_KEY`.

## Monitoring & Observability

Everything ships to **Grafana Cloud** through the `alloy` container
(`alloy/config.alloy`):

- **Metrics**: Prometheus scrapes of backend/worker/frontend/cadvisor/alloy
- **Logs**: JSON log files of all three apps tailed → Loki
- **Traces**: OTLP receiver on :4318/:4317 → Tempo (`O11Y_EXPORTER_ENDPOINT=alloy:4318`)
- **Profiles**: Pyroscope push receiver on :4040 (`O11Y_PROFILING_*`)
- **DB observability**: `database_observability.postgres` +
  `prometheus.exporter.postgres` using `POSTGRES_OBS_DSN`

Dashboards and alerts live as code in `grafana/` (jsonnet, `make build`);
product analytics dashboards in `posthog/dashboards/` (`node sync.mjs`).

## Backups

Per DECISIONS D2, the database has three protection layers (restore
procedures + quarterly drill: `../docs/runbooks/postgres-backup-restore.md`):

1. **Volume protection** — Postgres data lives in the `openmentor-postgres-data`
   volume, declared `external` in `docker-compose.yml` and created by the
   deploy scripts, so `docker compose down -v` can never delete it.
2. **Hetzner VM auto-backups** — enable them on the server (whole-VM
   snapshots, crash-consistent; Postgres WAL recovery makes them safe to
   restore from).
3. **Nightly logical dumps** — the `postgres-backup` sidecar runs
   `pg_dump -Fc` daily at `BACKUP_TIME` (default 03:30 UTC) and ships the
   dump to `s3://$BACKUP_S3_BUCKET/$BACKUP_S3_PREFIX/`, pruning objects older
   than `BACKUP_RETENTION_DAYS` (default 30). With no bucket configured it
   falls back to the local `openmentor-postgres-backups` volume and logs a
   loud warning. Manual/drill run:
   `docker exec openmentor-postgres-backup backup.sh once`.

RPO ≤ 24 h, RTO ≈ 30 min with dumps. Documented next step (not implemented):
**wal-g** continuous WAL archiving to S3 for ~minutes RPO / PITR. Scale path:
managed Postgres (Neon/RDS) — a `DATABASE_URL` swap to
`sslmode=verify-full` (the API image ships the CA in `certs/`).

Also back up:

- **Traefik certificates** — `traefik-letsencrypt-certificates` volume
  (recreatable; Let's Encrypt will reissue).
- **`.env.production`** — keep a copy in a password manager/secrets vault; it
  is the only non-reproducible config artifact.

## Known TODOs

- **P6.4 — registry swap**: images are still pushed to Yandex Container
  Registry (`cr.yandex`, service-account JSON key auth). Moving to **ghcr.io**
  (docker/login-action + PAT/OIDC) is tracked as P6.4; `TODO(P6.4)` markers
  sit in `.env.example`, `.env.production.example` and the deploy workflow.
  Until then the Yandex account must stay alive for pulls/pushes.
- **Image copy (D15)**: `migration/yandex-to-s3-migration.js` copies profile
  images from Yandex Object Storage to AWS S3; run before cutover.
- **wal-g PITR**: nightly `pg_dump` to S3 is implemented (see Backups);
  continuous WAL archiving with wal-g is the documented upgrade path if
  ~minutes RPO is ever needed.

## Security Considerations

- Only Traefik has public ports; backend/worker/alloy/cadvisor are internal.
- The worker requires `X-Worker-Token` (`WORKER_AUTH_TOKEN`) on all `/jobs/*`
  calls; the MCP endpoint requires `MCP_AUTH_TOKEN`.
- Never commit `.env` / `.env.production`; both are gitignored. Rotate tokens
  regularly and use different values per environment.
- On the VM: key-only SSH, UFW allowing 22/80/443, fail2ban recommended.

## Support

- Issues: https://github.com/openmentor-io/openmentor-infra/issues
- Docs: `DEPLOYMENT.md`, `ENVIRONMENT_VARIABLES.md`, `DOCKER_TAG_POLICY.md`,
  `docs/troubleshooting.md`, and the migration plan in `../docs/migration/`
