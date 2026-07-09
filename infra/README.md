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
| `traefik` | traefik:v2.11 | :80/:443 public | TLS termination (Let's Encrypt via Cloudflare DNS-01), routing. Dev overlay: HTTP-only on :80 |
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
infra/
├── docker-compose.yml          # Production stack
├── docker-compose.dev.yml      # Dev overlay (local image tags, HTTP-only traefik, dev postgres creds, opt-in observability)
├── postgres-backup/            # Backup sidecar image (pg_dump → S3, see Backups)
├── .env.example                # Local development env template
├── .env.production.example     # Production env template (deploy creds + build args + runtime secrets)
├── deploy.sh                   # Deploy [frontend|backend|infra|all] to the VM (health checks + auto-rollback)
├── deploy-dev.sh               # Same CLI/flow against the local docker stack
├── rollback.sh                 # Roll production back to previous image tags (per service)
├── alloy/config.alloy          # Grafana Alloy pipeline
├── grafana/                    # Dashboards & alerts as code (jsonnet → dist/)
├── posthog/dashboards/         # Product analytics dashboards as code
├── migration/                  # One-off Yandex Object Storage → AWS S3 image copy (D15)
├── DEPLOYMENT.md               # Production deployment guide
├── ENVIRONMENT_VARIABLES.md    # Env file layering explained
├── DOCKER_TAG_POLICY.md        # Why images are tagged with commit SHAs
└── docs/troubleshooting.md     # Operational troubleshooting
```

The sibling monorepo directories `../web` (frontend) and `../api` (Go API +
worker + migrations) are used for local builds — a single clone of the
monorepo brings everything.

## Quick Start (Local Development)

### Prerequisites

- Docker 20.10+ with Compose 2.x
- The monorepo cloned (one repo contains everything):

```
git clone https://github.com/openmentor-io/openmentor.git
openmentor/
├── web/     # frontend
├── api/     # backend + worker
└── infra/   # this directory
```

### 1. Start the stack

```bash
cd openmentor/infra
./deploy-dev.sh all --yes
```

`deploy-dev.sh` has the same CLI and flow as the production `deploy.sh`
(targets `frontend`/`backend`/`infra`/`all`, default `frontend backend`;
options `--tag`, `--yes`, `--dry-run`), but targets the local docker daemon:

1. creates `.env` from `.env.example` on first run (dev defaults +
   generated `JWT_SECRET`/`WORKER_AUTH_TOKEN`; fill in S3/SES/PostHog values
   for full functionality — never commit it),
2. builds `openmentor-frontend:dev-<sha>` / `openmentor-backend:dev-<sha>`
   from `../web` and `../api` (real unique tags, so `docker compose up -d`
   converges exactly like production: only services whose tag changed are
   recreated),
3. writes the tags to `.env` (`FRONTEND_IMAGE_TAG`/`BACKEND_IMAGE_TAG`),
4. converges the stack and runs the same health checks as production
   (frontend, backend, worker, postgres), rolling `.env` back to the
   previous tags on failure.

The dev overlay (`docker-compose.dev.yml`) keeps the **same service set as
production** — traefik / frontend / backend / worker / migrate / postgres —
with these differences:

- `traefik` runs HTTP-only on **:80** (no ACME/Cloudflare DNS-01, no :443)
  and routes `Host(localhost)` to the frontend,
- app ports (3000/8081/8090) are additionally published for debugging,
- `postgres` gets dev credentials, a disposable
  `openmentor-postgres-data-dev` volume and host port **5433**
  (`POSTGRES_DEV_PORT` overrides it if taken; only one database runs in the
  merged stack),
- `alloy` and `cadvisor` are **opt-in** via `--profile observability`:
  alloy cannot start without Grafana Cloud credentials and the
  `alloy-secrets/postgres_secret_openmentor` file, so it is not part of the
  default dev stack,
- `postgres-backup` never runs in dev (`profiles: [production-only]`) — dev
  data is disposable and the local stack must not touch backup buckets.

### 2. Access services

- Frontend via traefik: http://localhost/
- Frontend direct: http://localhost:3000
- Backend health: http://localhost:8081/api/healthcheck
- Worker health: http://localhost:8090/healthz
- Postgres: `psql postgresql://openmentor:password@localhost:5433/openmentor`

### 3. Day-to-day commands

```bash
COMPOSE="docker compose -f docker-compose.yml -f docker-compose.dev.yml"
$COMPOSE ps                      # status
$COMPOSE logs -f backend         # logs (any service)
$COMPOSE down                    # stop
$COMPOSE down && docker volume rm openmentor-postgres-data-dev   # reset dev DB
./deploy-dev.sh backend          # rebuild + roll just the backend/worker
./deploy-dev.sh all --dry-run    # print the plan without executing
```

## Environment Variables

See `ENVIRONMENT_VARIABLES.md` for how the env files layer, and the two
committed templates for the full annotated variable list:

- `.env.example` → `.env` (local development, read by compose `env_file`)
- `.env.production.example` → `.env.production` (deployment credentials,
  frontend build args, and runtime secrets; `deploy.sh` uploads it to
  `/opt/openmentor/infra/.env` on the VM)

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

Production is a single Hetzner Cloud VM (DECISIONS D2) with Docker, the
monorepo checked out at `/opt/openmentor` (compose runs from
`/opt/openmentor/infra`), and firewall open on 22/80/443.

### Deploy from a workstation

```bash
cp .env.production.example .env.production   # once; fill in everything
./deploy.sh                        # default targets: frontend backend
./deploy.sh frontend               # roll only the frontend
./deploy.sh backend                # roll backend + worker + migrate (one image)
./deploy.sh infra                  # sync infra/ config and converge compose changes
./deploy.sh all                    # frontend backend infra
./deploy.sh backend --tag abc123f  # deploy an already-pushed tag
./deploy.sh all --yes --dry-run    # print the plan / skip the prompt
./deploy.sh --staging              # target the staging VM (VM_SSH_*_STAGING vars)
```

`deploy.sh`:

1. builds the targeted images tagged with the monorepo's short commit SHA
   (see `DOCKER_TAG_POLICY.md` — never `latest`) and pushes them to the
   container registry (currently `cr.yandex`; ghcr.io swap tracked as
   **P6.4**),
2. fetches the currently deployed tags from the VM for any service **not**
   being deployed, so untouched services keep their tags,
3. for the `infra` target: rsyncs `infra/` to `/opt/openmentor/infra`
   (never `.env*`, `logs/`, `alloy-secrets/`; no `--delete`) with
   `--checksum --itemize-changes`, so it knows which files actually changed,
4. uploads `.env.production` to the VM as `/opt/openmentor/infra/.env`
   (mode 600) with the resolved image tags, and writes the Alloy
   DB-observability secret,
5. ensures the external `openmentor-postgres-data` volume exists
   (idempotent `docker volume create`), then runs
   `docker-compose pull && up -d` on the VM (`--remove-orphans` when the
   `infra` target is included) — compose convergence recreates **only** the
   services whose image tag or definition changed,
6. handles the **bind-mount trap**: compose does not react to changes in
   bind-mounted config files, so after `up` the script restarts/rebuilds
   exactly the affected services (see inventory below),
7. health-checks frontend (`/api/healthcheck`), backend (`/api/healthcheck`),
   worker (`/healthz`) and postgres (`pg_isready`) **inside** the containers
   plus the backup sidecar's running state, and
8. **automatically rolls back** to the previous `.env` (previous image tags)
   if any health check fails, then verifies the rollback.

`deploy-dev.sh` runs the identical CLI and flow against the local dev stack
(no registry, no SSH — see Quick Start).

#### Bind-mounted config inventory (what `infra` restarts/rebuilds)

| Service | Config source | On change |
|---|---|---|
| `alloy` | `./alloy/config.alloy` (bind-mounted file) | `docker-compose restart alloy` |
| `alloy` | `./alloy-secrets/` (runtime state written by deploy.sh) | never synced |
| `postgres-backup` | built **on the VM** from `./postgres-backup/` | `docker-compose build postgres-backup` + up |
| `traefik` | none — static config is command flags, dynamic config is docker labels | plain compose convergence |

Compose-level changes (bumped `traefik`/`postgres`/`alloy` image pins,
service definitions, env passthrough) converge through `up -d` itself.
**Postgres pin bumps are safe**: the container is recreated but the data
lives in the external `openmentor-postgres-data` volume. Minor/patch
versions only — major upgrades follow
`../docs/runbooks/postgres-backup-restore.md`.

### Rollback manually

```bash
./rollback.sh <previous-commit-sha>            # both images
./rollback.sh --frontend <sha>                 # frontend only
./rollback.sh --backend <sha>                  # backend/worker/migrate only
```

See `DEPLOYMENT.md` for the full guide and troubleshooting.

## CI/CD Pipeline

`../.github/workflows/deploy.yml` (repo root) builds/pushes both images and deploys over SSH
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

- Issues: https://github.com/openmentor-io/openmentor/issues
- Docs: `DEPLOYMENT.md`, `ENVIRONMENT_VARIABLES.md`, `DOCKER_TAG_POLICY.md`,
  `docs/troubleshooting.md`, and the migration plan in `../docs/migration/`
