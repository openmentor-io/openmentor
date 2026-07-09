# Production Deployment Guide

This guide explains how to deploy OpenMentor to the production VM from your
local machine using `./deploy.sh`.

## Prerequisites

1. **Docker** installed and running locally
2. **SSH access** to the production VM (Hetzner Cloud, see DECISIONS D2)
3. **Container registry credentials** — currently Yandex Container Registry
   (`TODO(P6.4)`: moving to ghcr.io):
   - Service account JSON key with `container-registry.images.pusher` role
   - Registry ID (`crp...`)
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

- **Deployment machine settings** — used only by `deploy.sh`/`rollback.sh`
  locally: `YANDEX_SA_KEY_FILE`, `YANDEX_REGISTRY_ID`, `VM_SSH_HOST`,
  `VM_SSH_USER`, `VM_SSH_KEY_FILE`
- **Build-time variables** — `NEXT_PUBLIC_*` (and Faro/PostHog sourcemap
  vars), baked into the frontend image during `docker build`
- **Runtime secrets** — everything the containers read from
  `/opt/openmentor/infra/.env` on the VM (`DATABASE_URL`, `S3_STORAGE_*`,
  `SES_*`, `WORKER_AUTH_TOKEN`, trigger URLs, `JWT_SECRET`, `MCP_AUTH_TOKEN`,
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
./deploy.sh                    # build + deploy frontend AND backend/worker
./deploy.sh --frontend-only    # only rebuild frontend, keep current backend
./deploy.sh --backend-only     # only rebuild backend, keep current frontend
./deploy.sh --staging          # deploy to the staging VM (VM_SSH_*_STAGING vars)
```

The script will:

1. Validate credentials and config
2. Build Docker images locally (frontend and/or backend), tagged with the
   monorepo's short commit SHA (`DOCKER_TAG_POLICY.md`)
3. Push images to the registry
4. Upload `.env.production` to the VM as `/opt/openmentor/infra/.env`
   (mode 600) with `FRONTEND_IMAGE_TAG`/`BACKEND_IMAGE_TAG` appended
5. Write the Alloy DB-observability secret (`POSTGRES_OBS_DSN`) to
   `alloy-secrets/` on the VM
6. Run `docker-compose pull && docker-compose up -d` on the VM
7. Health-check all three apps inside their containers:
   - frontend `http://localhost:3000/api/healthcheck`
   - backend `http://localhost:8081/api/healthcheck`
   - worker `http://localhost:8090/healthz`
8. **Automatically roll back** (restore the previous `.env`, re-pull, re-up)
   if any health check fails
9. Verify the public endpoint `https://$DOMAIN/api/healthcheck`

Note: the `migrate` service runs database migrations before backend and
worker start (`depends_on: service_completed_successfully`).

## Rollback

```bash
./rollback.sh <commit-sha>   # or run without args to be prompted
```

Reads the same `.env.production`, SSHes to the VM, sets `IMAGE_TAG` to the
given SHA, pulls, restarts, and verifies the three health checks.

Manual fallback on the VM:

```bash
cd /opt/openmentor/infra
export IMAGE_TAG=<previous-working-sha>
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
# Test registry login manually (Yandex CR until P6.4)
cat /path/to/sa-key.json | docker login --username json_key --password-stdin cr.yandex
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

- Test locally first: `./deploy-dev.sh` or `./dev.sh up-d`
- Deploy from a clean, committed tree
- Note the deployed SHAs (printed in the summary) for quick rollback
- Watch Grafana for 5–10 minutes after deploying

## Related Documentation

- [README.md](README.md) — architecture and stack overview
- [ENVIRONMENT_VARIABLES.md](ENVIRONMENT_VARIABLES.md) — env file layering
- [DOCKER_TAG_POLICY.md](DOCKER_TAG_POLICY.md) — image tagging strategy
- [../.github/workflows/deploy.yml](../.github/workflows/deploy.yml) — CI deploy
  (manual `workflow_dispatch`)
