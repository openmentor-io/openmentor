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
├── env-allowlist.txt         # Per-service allowlist of env KEY names (committed)
├── check-service-env.sh      # Enforces the allowlist against docker-compose.yml
└── docker-compose.yml        # Explicit per-service `environment:` blocks
```

`.env` is the single source of truth. Compose reads it for **interpolation**
(image tags in `image:`, `${DOMAIN}` in labels, `${DATABASE_URL}` in a service's
`environment:`) — it is never handed to a container wholesale.

Each service declares an explicit `environment:` allowlist. Two forms appear:

| Form | Meaning |
|---|---|
| `- KEY=value` / `- KEY=${KEY:-default}` | fixed or defaulted value |
| `- KEY` (bare) | value taken from `.env`; **stays unset** in the container when `.env` has no such line |

The bare form is deliberate: `- KEY=${KEY}` renders as `KEY: ""` for a key
`.env` does not define (plus a `The "KEY" variable is not set` warning on every
compose invocation), while a bare `- KEY` renders as `KEY: null` and is not
passed to the container at all — exactly the old `env_file` semantics.

What that does **not** break is the Go defaults: viper's `AutomaticEnv` with the
default `allowEmptyEnv=false` reports an empty variable as *absent*
(`viper.go`: `return val, ok && (v.allowEmptyEnv || val != "")`, and
`api/config/config.go` never calls `AllowEmptyEnv`), so `SetDefault` still wins.
What it breaks is diagnosis: every optional key would arrive as an empty
variable, and the troubleshooting bundle's "which variables arrived empty" check
(`infra/docs/troubleshooting.md`) would list dozens of them and lose its signal.

The same `KEY: null` behaviour is a trap for the allowlist check: compose renders
an unresolvable bare entry away entirely (v2 deletes it, v5 keeps a null), so a
`- NEW_SECRET` defined only in the production `.env` would be invisible to a CI
run that interpolates `.env.example`. `check-service-env.sh` therefore parses the
keys the compose file *declares* and seeds every bare one before rendering; its
`--self-test` injects a value-less entry to prove that path still bites.

> **Removed (SECURITY P10): `.env.runtime`.** Six services shared
> `env_file: .env.runtime`, a copy of the whole production env, so every
> container held every secret: the internet-facing frontend had `DATABASE_URL`,
> `POSTGRES_PASSWORD`, `JWT_SECRET`, `WORKER_AUTH_TOKEN`, the SES/S3/backup
> keys, `CLOUDFLARE_DNS_API_TOKEN` and `GCLOUD_RW_API_KEY` while sharing a
> network with `postgres`. The deploy scripts delete the file on the first
> deploy that ships this `docker-compose.yml` to the VM, and keep regenerating
> it until then — compose treats `env_file` as required, so removing it while
> the VM still runs the pre-P10 compose file aborts the deploy (see "First
> deploy after the per-service env allowlists" in `DEPLOYMENT.md`). Image tags
> are still absent from container env, so a tag-only deploy still recreates
> only the retagged service.

### Changing the contract

Adding an env var to a service takes **two** edits in the same commit:
`docker-compose.yml` and `infra/env-allowlist.txt`. Then:

```bash
cd infra && ./check-service-env.sh              # subset + secret-ownership check
cd infra && ./check-service-env.sh --self-test  # also proves the check still bites
```

The script renders compose to JSON and compares **key names only** — it never
reads or prints a value, so it is safe to run against a production `.env`. CI
runs it on every PR that touches `infra/`, as part of `make check` in the
required `Checks / required-checks` gate, so a forbidden key added to a service
fails the PR even when nothing outside `infra/` changed.

A `.env` you copied from `.env.example` purely to render compose is a secret
file with a lifetime, not a build artifact — `.env` is the one file that holds
real credentials once you fill it in, and a stray copy is what leaks. Delete it
when you finish; `make check` falls back to `.env.example` on its own, and
`./deploy-dev.sh` regenerates a dev `.env`, so nothing is lost:

```bash
rm -f .env     # from infra/, when you only needed it to render compose
```

## Environment Files Explained

### 1. `.env` (Local Development)

**Purpose**: runtime configuration for the local compose stack
**Used by**: compose interpolation (which feeds the per-service `environment:` allowlists), `deploy-dev.sh` (which creates it from `.env.example` with dev defaults and writes the `FRONTEND_IMAGE_TAG`/`BACKEND_IMAGE_TAG` lines)
**Git status**: ignored

Contains dev database URL (the dev overlay runs `postgres` on host port
5433), S3/SES credentials (or stubs), auth tokens, and optional Grafana
Cloud / PostHog keys.

### 2. `.env.production` (Production — one file for everything)

**Purpose**: single source for production deployment
**Used by**: `deploy.sh` and `rollback.sh` locally; uploaded by `deploy.sh`
to `/opt/openmentor/infra/.env` on the VM, where compose interpolates it into
each service's `environment:` allowlist (no container reads the file itself)
**Git status**: ignored

Three sections (mirroring `.env.production.example`):

1. **Deployment machine configuration** — the ECR registry (`ECR_REGISTRY`,
   `AWS_REGION` — AWS ECR per DECISIONS D19), the VM's pull-only ECR
   no VM-side credentials (each deploy mints a short-lived ECR token locally
   key of the `openmentor-vm` IAM user; the deploy scripts read them from
   the uploaded `.env` ON THE VM and run
   `aws ecr get-login-password | docker login` before pulling) and VM SSH
   settings (`VM_SSH_HOST/USER/KEY_FILE`, plus `VM_SSH_*_STAGING` for
   `--staging`)
2. **Build-time variables** — `NEXT_PUBLIC_*` values baked into the frontend
   image (plus optional Faro/PostHog sourcemap-upload vars)
3. **Runtime secrets** — read by the containers at startup

The deploy script appends `FRONTEND_IMAGE_TAG`/`BACKEND_IMAGE_TAG`
automatically — never set image tags manually in the file.

## Build-Time vs Runtime

### Build-time (frontend only)

`NEXT_PUBLIC_*` variables are **baked into** the frontend image during
`docker build` (see `ARG`s in `../web/Dockerfile`). Changing them
requires rebuilding the frontend image (`./deploy.sh frontend`).

### Runtime (per container, allowlisted)

Everything else is injected per service by the `environment:` blocks in
`docker-compose.yml`. Changing a runtime value does **not** require an image
rebuild — update `.env` and `docker compose up -d` (or re-run `./deploy.sh`).

Compose resolution order (highest priority first): shell env → `environment:`
entries in compose → image defaults.

## Service → key matrix

Authoritative machine-readable copy: [`env-allowlist.txt`](env-allowlist.txt),
enforced by [`check-service-env.sh`](check-service-env.sh). Sensitive keys are
**bold**; a blank cell means the key never enters that container.

| Key | traefik | docker-socket-proxy | frontend | postgres | postgres-backup | migrate | backend | worker | alloy |
|---|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|:-:|
| `CONTAINERS`, `EVENTS`, `PING`, `VERSION`³ | | ● | | | | | | | |
| `APP_ENV`, `LOG_LEVEL` | | | ● | | | ● | ● | ● | ● |
| `DEPLOYMENT_NAME` | | | | | | | | | ● |
| `LOG_DIR` | | | ● | | | ● | ● | ● | |
| `PORT` / `WORKER_PORT` | | | ● | | | | ● | ● | ● |
| `SERVICE_INSTANCE_ID` | | | ● | | | | ● | ● | |
| `GOMEMLIMIT` | | | | | | | ●² | ●² | |
| `NODE_ENV`, `NEXT_PUBLIC_*` | | | ● | | | | | | |
| `DOMAIN` | | | ● | | | | | | |
| **`GO_API_INTERNAL_TOKEN`** | | | ● | | | | | | |
| **`METRICS_AUTH_TOKEN`** | | | ● | | | | | | ● |
| **`CLOUDFLARE_DNS_API_TOKEN`** | ● | | | | | | | | |
| **`POSTGRES_USER/PASSWORD/DB`** | | | | ● | ● | | | | |
| `POSTGRES_HOST`, `BACKUP_*` | | | | | ● | | | | |
| **`DATABASE_URL`** | | | | | | ● | ● | ● | |
| `BASE_URL`, `ALLOWED_CORS_ORIGINS`, `DB_WORK_OFFLINE` | | | | | | ● | ● | ● | |
| `TRUSTED_PROXIES` | | | | | | | ● | | |
| **`INTERNAL_MENTORS_API`**, **`MENTORS_API_LIST_AUTH_TOKEN`**, **`TURNSTILE_SECRET_KEY`**, **`JWT_SECRET`** | | | | | | ○ | ● | ○ | |
| `JWT_ISSUER`, `SESSION_TTL_HOURS`, `LOGIN_TOKEN_TTL_MINUTES`, `COOKIE_*` | | | | | | | ● | | |
| **`WORKER_AUTH_TOKEN`** | | | | | | ○ | ● | ● | |
| `*_TRIGGER_URL` | | | | | | | ● | | |
| **`S3_STORAGE_*`** | | | | | | ○ | ● | ○ | |
| **`SES_*`**, `MODERATORS_EMAIL`, `DISCORD_MENTORS_PRIVATE_INVITE_LINK`, `DEV_EMAIL_OVERRIDE` | | | | | | | | ● | |
| `WORKER_CRON_ENABLED`, `WORKER_DB_MAX_CONNS`, `HIGHLIGHTED_MENTORS` | | | | | | | | ● | |
| `WORKER_PROFILE_PURGE_CRON`, `WORKER_PROFILE_PURGE_RETENTION_DAYS` | | | | | | | | ● | |
| `ANALYTICS_*`, **`POSTHOG_API_KEY`**, `POSTHOG_HOST/ENABLED/CAPTURE_ENDPOINT` | | | | | | ○ | ● | ● | |
| `POSTHOG_DISABLE_GEOIP` | | | | | | | ● | ● | |
| `O11Y_EXPORTER_ENDPOINT` | | | ● | | | | ● | ● | |
| `O11Y_SERVICE_NAMESPACE` | | | ● | | | | ● | ● | ● |
| `O11Y_FE_SERVICE_NAME` | | | ● | | | | | | ● |
| `O11Y_FE_SERVICE_VERSION` | | | ● | | | | | | |
| `O11Y_BE_SERVICE_NAME` | | | | | | | ● | | ● |
| `O11Y_BE_SERVICE_VERSION` | | | | | | | ● | ● | |
| `O11Y_WORKER_SERVICE_NAME` | | | | | | | | ● | ● |
| `O11Y_PROFILING_*` | | | | | | | ● | ●¹ | |
| **`GCLOUD_*`** | | | | | | | | | ● |
| **`POSTGRES_OBS_DSN`** | | | | | | | | | ● |
| `PROMETHEUS_SCRAPE_INTERVAL` | | | | | | | | | ● |

¹ the worker gets all `O11Y_PROFILING_*` except `O11Y_PROFILING_APP_NAME`: it
names its profile stream from `O11Y_WORKER_SERVICE_NAME` so its profiles never
mix with the API's.

² fixed in `docker-compose.yml`, not read from `.env`. Go's GC targets ~2x live
heap and ignores the cgroup limit, so without it a healthy heap can still get
the container OOM-killed. Each value is ~20% under that service's own
`mem_limit` (backend 512m → `400MiB`, worker 256m → `200MiB`) to leave room for
non-heap memory — change one and change the other, or the soft limit lands
above the hard one and stops doing anything.

³ `DB_WORK_OFFLINE` reaches the worker only as an accepted override; nothing
implements an offline mode (see M7).

There is no longer a "validation-only" column in this matrix. Every key each
binary receives is one it reads: `api/config` has a per-binary validation profile
(`ValidateForAPI` / `ValidateForWorker` / `ValidateForMigrate`, D62), so
`cmd/migrate` holds `DATABASE_URL` and no credential at all, and `cmd/worker`
holds no S3, Turnstile, mentors-API or session secret.

That column existed because all three binaries shared one `Validate()`, and the
coupling was not theoretical: on 2026-08-04 a storage check written for `cmd/api`
made `migrate` exit 1 and `depends_on: service_completed_successfully` held the
whole stack in Created (a57aec2). **If a new check makes one binary demand a
setting it does not read, add it to that binary's profile — do not widen this
matrix.**
³ endpoint switches for the filtered Docker API, not credentials — see
[`docker-compose.yml`](docker-compose.yml) for what each one lets Traefik call.
Everything else in that image defaults to `0`, POST included, so this list *is*
the allowlist and adding to it widens what a Traefik RCE can reach.

○ = passed **only** to satisfy `config.Validate()`. `cmd/migrate` and
`cmd/worker` call the same `config.Load()` as `cmd/api`, which rejects a missing
`INTERNAL_MENTORS_API` / `MENTORS_API_LIST_AUTH_TOKEN` / `TURNSTILE_SECRET_KEY`
/ `JWT_SECRET` / `WORKER_AUTH_TOKEN` / `POSTHOG_API_KEY`. Neither binary reads
them. Trimming these needs per-binary validation profiles in `api/config` —
an API change, tracked separately, not something compose can fix.

### Never in any container

Deploy- and build-only values, kept out of runtime env entirely:

| Key | Used by |
|---|---|
| `MIGRATE_DATABASE_URL`, `API_DATABASE_URL`, `WORKER_DATABASE_URL` | compose interpolation: each SELECTS the value of `DATABASE_URL` for one container (`${API_DATABASE_URL:-${DATABASE_URL}}`), so the container still sees a single `DATABASE_URL` and the matrix above is unchanged. SECURITY (H8) |
| `BACKUP_POSTGRES_USER`, `BACKUP_POSTGRES_PASSWORD` | compose interpolation: the same trick for the sidecar's `POSTGRES_USER`/`POSTGRES_PASSWORD` |
| `ECR_REGISTRY`, `AWS_REGION`, `IMAGE_TAG`, `FRONTEND_IMAGE_TAG`, `BACKEND_IMAGE_TAG` | compose interpolation / deploy scripts |
| `VM_SSH_HOST`, `VM_SSH_USER`, `VM_SSH_KEY_FILE` | `deploy.sh` / `rollback.sh` on your workstation |
| `LETSENCRYPT_EMAIL` | traefik `command:` interpolation (static config, not env) |
| `POSTHOG_PERSONAL_API_KEY`, `POSTHOG_PROJECT_ID` | `next build` sourcemap upload (`next.config.js`) |
| `FARO_API_KEY`, `FARO_API_ENDPOINT`, `FARO_APP_ID`, `FARO_STACK_ID` | `next build` sourcemap upload |

Dropping `POSTHOG_PERSONAL_API_KEY` / `POSTHOG_PROJECT_ID` from the frontend's
runtime env changes nothing: `web/` builds with `output: 'standalone'`, and the
`server.js` Next generates inlines the evaluated config as a literal
(`const nextConfig = {…}`) instead of requiring `next.config.js`. The container
runs `node server.js`, so the file — and the `withPostHogConfig` wrapper it
applies, which only configures build-time sourcemap upload
(`@posthog/nextjs-config` is a devDependency) — is never on the runtime path.
The same is true of every `NEXT_PUBLIC_*` value: baked in at build time. A
full-access personal API key has no business in a public-facing container.

## Security Best Practices

1. Never commit `.env` or `.env.production` (both gitignored)
2. Use different credentials per environment
3. Rotate tokens regularly; generate with `openssl rand -base64 32`.
   Procedure and ordering: [`../docs/runbooks/secret-rotation.md`](../docs/runbooks/secret-rotation.md)
4. On the VM: `chmod 600 /opt/openmentor/infra/.env`
5. Back up `.env.production` in a password manager or secrets vault — never
   in email/chat/repos
6. Never paste `env` output into an issue, email or chat. To check whether a
   value reached a container, test presence, not content:
   `docker exec openmentor-backend sh -c 'test -n "$DATABASE_URL" && echo SET || echo UNSET'`

## Troubleshooting

### Backend can't connect to PostgreSQL

Check that the DSN is *present*, never what it contains — it embeds the database
password, and this output ends up pasted into issues and emails:

```bash
docker exec openmentor-backend sh -c 'test -n "$DATABASE_URL" && echo DATABASE_URL=SET || echo DATABASE_URL=UNSET'
docker logs openmentor-migrate    # migrations run first; failures block startup
docker exec openmentor-postgres pg_isready -U openmentor -d openmentor
```

### Frontend can't call backend / stale public config

`NEXT_PUBLIC_*` values are baked at build time — rebuild the frontend image
(`./deploy.sh frontend`); re-uploading `.env` is not enough.

### Variable not taking effect

1. Build-time (`NEXT_PUBLIC_*`)? → rebuild the frontend image
2. Is the key in that service's `environment:` block? A key present in `.env`
   but missing from the block never reaches the container — that is the point.
   `grep -A80 '^  <service>:' docker-compose.yml | grep <KEY>`, and add it to
   `env-allowlist.txt` in the same commit.
3. Did it reach the VM? Check for the line, not the value:
   `ssh <vm> "grep -c '^VAR=' /opt/openmentor/infra/.env"`
4. Then `docker compose up -d` to recreate the container.

## Summary

| File | Purpose | Lives | Used by |
|------|---------|-------|---------|
| `.env` | Runtime config + image tags | local / VM | compose interpolation, deploy scripts |
| `.env.production` | Deploy creds + build args + prod runtime | local + uploaded to VM as `.env` | `deploy.sh`, `rollback.sh` |
| `env-allowlist.txt` | Which env keys each service may receive | committed | `check-service-env.sh` |
