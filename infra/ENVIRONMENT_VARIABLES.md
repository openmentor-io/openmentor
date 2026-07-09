# Environment Variables Guide

## Overview

OpenMentor uses two env files — one for local development, one for production
deployment. The committed `*.example` templates are the authoritative,
annotated list of every variable.

## File Structure

```
infra/
├── .env                      # Local development (git ignored)
├── .env.example              # Development template (committed)
├── .env.production           # Production: deploy creds + build args + runtime secrets (git ignored)
├── .env.production.example   # Production template (committed)
└── docker-compose.yml        # env_file: .env
```

## Environment Files Explained

### 1. `.env` (Local Development)

**Purpose**: runtime configuration for the local compose stack
**Used by**: `docker-compose.yml` + `docker-compose.dev.yml` (`env_file: .env`), `dev.sh`, `deploy-dev.sh`
**Git status**: ignored

Contains dev database URL (the dev overlay runs `postgres` on host port
5433), S3/SES credentials (or stubs), auth tokens, and optional Grafana
Cloud / PostHog keys.

### 2. `.env.production` (Production — one file for everything)

**Purpose**: single source for production deployment
**Used by**: `deploy.sh` and `rollback.sh` locally; uploaded by `deploy.sh`
to `/opt/openmentor/infra/.env` on the VM where **all containers** read it
**Git status**: ignored

Three sections (mirroring `.env.production.example`):

1. **Deployment machine configuration** — registry credentials
   (`YANDEX_SA_KEY_FILE`, `YANDEX_REGISTRY_ID` — ghcr.io swap tracked as
   P6.4) and VM SSH settings (`VM_SSH_HOST/USER/KEY_FILE`, plus
   `VM_SSH_*_STAGING` for `--staging`)
2. **Build-time variables** — `NEXT_PUBLIC_*` values baked into the frontend
   image (plus optional Faro/PostHog sourcemap-upload vars)
3. **Runtime secrets** — read by the containers at startup

The deploy script appends `FRONTEND_IMAGE_TAG`/`BACKEND_IMAGE_TAG`
automatically — never set image tags manually in the file.

## Build-Time vs Runtime

### Build-time (frontend only)

`NEXT_PUBLIC_*` variables are **baked into** the frontend image during
`docker build` (see `ARG`s in `../web/Dockerfile`). Changing them
requires rebuilding the frontend image (`./deploy.sh --frontend-only`).

### Runtime (all containers)

Everything else is read at container startup from
`/opt/openmentor/infra/.env` via compose `env_file`. Changing runtime values
does **not** require an image rebuild — re-run `./deploy.sh` (or edit the
`.env` on the VM and `docker-compose up -d`).

Compose resolution order (highest priority first): shell env →
`environment:` entries in compose → `env_file` → image defaults.

## Required Variables by Service

### Frontend container

- Build-time: `NEXT_PUBLIC_GO_API_URL`, `NEXT_PUBLIC_S3_STORAGE_*`,
  `NEXT_PUBLIC_CDN_ENDPOINT`, `NEXT_PUBLIC_RECAPTCHA_V2_SITE_KEY`,
  `NEXT_PUBLIC_ANALYTICS_*`, `NEXT_PUBLIC_POSTHOG_*`, `NEXT_PUBLIC_O11Y_*`,
  optional `NEXT_PUBLIC_FARO_*`
- Runtime: `GO_API_INTERNAL_TOKEN` (internal API calls),
  `METRICS_AUTH_TOKEN` (protects `/api/metrics`)

### Backend (Go API) container

- `DATABASE_URL`, `S3_STORAGE_*`
- Required by config validation: `INTERNAL_MENTORS_API`,
  `MENTORS_API_LIST_AUTH_TOKEN`,
  `MCP_AUTH_TOKEN`, `RECAPTCHA_V2_SECRET_KEY`
- `JWT_SECRET` (mentor/admin passwordless-login sessions)
- Worker triggers: `WORKER_AUTH_TOKEN` + `*_TRIGGER_URL` pointing at
  `http://worker:8090/jobs/...`
- Analytics: `ANALYTICS_PROVIDER`, `POSTHOG_API_KEY`, `POSTHOG_HOST`
- Observability: `O11Y_BE_SERVICE_NAME`, `O11Y_EXPORTER_ENDPOINT`,
  `O11Y_PROFILING_*`

### Worker container (same image, `/app/worker`)

- Shares the backend's `.env`: `DATABASE_URL`, `WORKER_AUTH_TOKEN`,
  `WORKER_CRON_ENABLED`, `HIGHLIGHTED_MENTORS`
- Email (AWS SES, DECISIONS D1): `SES_REGION`, `SES_ACCESS_KEY_ID`,
  `SES_SECRET_ACCESS_KEY`, optional `SES_ENDPOINT`, `MODERATORS_EMAIL`,
  `DEV_EMAIL_OVERRIDE` (non-production reroute — must be empty in prod)
- Observability: `O11Y_WORKER_SERVICE_NAME` (also its profiling app name),
  `O11Y_EXPORTER_ENDPOINT`

### Grafana Alloy container

- `GCLOUD_HOSTED_{METRICS,LOGS,TRACES,PROFILES}_{URL,ID}`,
  `GCLOUD_RW_API_KEY`
- `METRICS_AUTH_TOKEN` (bearer token for scraping frontend metrics)
- `O11Y_*_SERVICE_NAME`, `O11Y_SERVICE_NAMESPACE`,
  `PROMETHEUS_SCRAPE_INTERVAL`
- `POSTGRES_OBS_DSN` (dedicated `grafana_monitoring` user; `deploy.sh` writes
  it to `alloy-secrets/` on the VM)

## Security Best Practices

1. Never commit `.env` or `.env.production` (both gitignored)
2. Use different credentials per environment
3. Rotate tokens regularly; generate with `openssl rand -base64 32`
4. On the VM: `chmod 600 /opt/openmentor/infra/.env`
5. Back up `.env.production` in a password manager or secrets vault — never
   in email/chat/repos

## Troubleshooting

### Backend can't connect to PostgreSQL

```bash
docker exec openmentor-backend env | grep DATABASE_URL
docker logs openmentor-migrate    # migrations run first; failures block startup
```

### Frontend can't call backend / stale public config

`NEXT_PUBLIC_*` values are baked at build time — rebuild the frontend image
(`./deploy.sh --frontend-only`); re-uploading `.env` is not enough.

### Variable not taking effect

1. Build-time (`NEXT_PUBLIC_*`)? → rebuild the frontend image
2. Runtime? → confirm it reached the VM: `ssh <vm> "grep ^VAR /opt/openmentor/infra/.env"`,
   then `docker-compose up -d` to recreate containers
3. Check for a hardcoded `environment:` override in `docker-compose.yml`
   (those beat `env_file`)

## Summary

| File | Purpose | Lives | Used by |
|------|---------|-------|---------|
| `.env` | Local dev runtime | local only | compose (dev overlay) |
| `.env.production` | Deploy creds + build args + prod runtime | local + uploaded to VM as `.env` | `deploy.sh`, `rollback.sh`, all prod containers |
