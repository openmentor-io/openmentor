[![Build and Test](https://github.com/openmentor-io/openmentor/api/actions/workflows/build-and-test.yml/badge.svg)](https://github.com/openmentor-io/openmentor/api/actions/workflows/build-and-test.yml)
[![PR Checks](https://github.com/openmentor-io/openmentor/api/actions/workflows/pr-checks.yml/badge.svg)](https://github.com/openmentor-io/openmentor/api/actions/workflows/pr-checks.yml)

# OpenMentor API (Go)

Backend for [openmentor.io](https://openmentor.io): a Go API server plus a
background worker, backed by PostgreSQL. One Docker image ships three
binaries; the compose service picks the command:

| Binary | Path in image | Role |
|---|---|---|
| `cmd/api` | `/app/openmentor-api` | HTTP API (port 8081, internal-only behind Traefik) |
| `cmd/worker` | `/app/worker` | Background jobs: event triggers + cron (port 8090, internal-only) |
| `cmd/migrate` | `/app/migrate` | Runs `migrations/` (golang-migrate), executed before api/worker start |

## Layout

```
├── cmd/                  # api, worker, migrate entry points
├── config/               # env-driven configuration (viper)
├── internal/
│   ├── cache/            # in-memory mentor/tags caches
│   ├── handlers/         # HTTP handlers
│   ├── middleware/       # auth, sessions, rate limiting, observability
│   ├── models/           # data models
│   ├── repository/       # PostgreSQL data access (pgx)
│   ├── services/         # business logic
│   └── worker/           # worker HTTP server, job handlers, cron scheduler
├── pkg/                  # db pool, email (SESv2), s3storage, analytics,
│                         # jwt, logger, metrics, tracing, trigger, ...
├── migrations/           # SQL schema migrations
└── test/                 # tests mirroring the source tree
```

## Auth model

- **Public list endpoints** — `mentors_api_auth_token` header
  (`MENTORS_API_LIST_AUTH_TOKEN`).
- **Internal endpoints** — `x-internal-mentors-api-auth-token` header
  (`INTERNAL_MENTORS_API`).
- **Mentor portal / admin moderation** — passwordless email login: a
  single-use login token (expires in `LOGIN_TOKEN_TTL_MINUTES`, default 15)
  is emailed via the worker, then exchanged for a JWT session in an HttpOnly
  cookie (`JWT_SECRET`, `SESSION_TTL_HOURS`). Login endpoints are rate
  limited to ~2 req/5 min per IP.
- **API → worker** — the API sends `X-Worker-Token` (`WORKER_AUTH_TOKEN`)
  on every trigger call; the worker rejects `/jobs/*` requests without it.

## API endpoints

Public (token header):
- `GET /api/mentors` — all visible mentors
- `GET /api/mentor/:id` — single mentor
- `POST /api/contact-mentor` — contact form (Turnstile verified)
- `POST /api/register-mentor` — mentor registration (Turnstile verified)
- `POST /api/logs` — frontend log ingestion
- `GET /api/reviews/:requestId/check`, `POST /api/reviews/:requestId` — mentee reviews (Turnstile verified)

Mentor portal (`/api/v1/auth/mentor/*` + session cookie):
- `request-login`, `verify`, `logout`, `session`
- `GET|POST /api/v1/mentor/profile`, `POST .../profile/status`, `POST .../profile/picture`
- `GET /api/v1/mentor/requests[/:id]`, `POST .../requests/:id/status`, `POST .../requests/:id/decline`

Admin moderation (`/api/v1/auth/admin/*` + session cookie):
- `GET /api/v1/admin/mentors[/:id]`, `POST .../mentors/:id`,
  `.../approve`, `.../decline`, `.../status`, `.../picture`

Internal / utility:
- `POST /api/internal/mentors` — cached mentor API for the frontend
- `GET /api/healthcheck`, `GET /api/metrics` (Prometheus)

## Background worker

`cmd/worker` replaces the deprecated `openmentor-func` Azure Functions app.
It is an internal-only HTTP server plus a cron scheduler, and sends all
transactional email through AWS SESv2 (`pkg/email`; templates in
`pkg/email/templates/assets`).

**Event jobs** — the API fires these asynchronously via `pkg/trigger` after
database writes; configure the `*_TRIGGER_URL` vars to point at the worker
(see `.env.example`):

```
POST|GET /jobs/new-mentor-watcher?mentorId=
POST|GET /jobs/new-request-watcher?requestId=
POST     /jobs/mentor-login-email        (JSON body)
POST     /jobs/moderator-login-email     (JSON body)
POST     /jobs/mentor-moderation-action  (JSON body)
POST|GET /jobs/process-mentee-review?reviewId=
GET      /jobs/request-process-finished?requestId=
```

**Cron jobs** (scheduled when `WORKER_CRON_ENABLED=true`):

| Job | Schedule |
|---|---|
| `sessions-watcher` | daily 08:30 |
| `update-status-reminder` | Wednesdays 10:00 |
| `deactivate-pending-mentors` | Wednesdays 10:00 |
| `randomize-sort-order` | daily 01:00 (pins `HIGHLIGHTED_MENTORS` on top) |

Every cron job can also be run manually — `POST /jobs/cron/<name>` (same
`X-Worker-Token` guard) returns the run summary as JSON:

```bash
curl -X POST -H "X-Worker-Token: $WORKER_AUTH_TOKEN" \
  http://localhost:8090/jobs/cron/sessions-watcher
```

The worker serves `/healthz` and `/metrics`, keeps its own smaller DB pool
(`WORKER_DB_MAX_CONNS`), and the email cron jobs only send in
`APP_ENV=production` unless `DEV_EMAIL_OVERRIDE` reroutes all recipients.

## Storage & integrations

- **PostgreSQL** via `jackc/pgx` (`DATABASE_URL`). TLS follows standard
  pgx/libpq DSN semantics: the self-hosted compose Postgres uses
  `sslmode=disable`; managed-PG users set `sslmode=verify-full` and pass the
  provider CA via `sslrootcert=<path>` in `DATABASE_URL` (or rely on the
  system trust store).
- **Profile pictures** on any S3-compatible storage (`pkg/s3storage`,
  `S3_STORAGE_*` — Cloudflare R2, AWS S3, Backblaze B2, ...).
- **Email** via AWS SESv2 (`SES_*`, `MODERATORS_EMAIL`).
- **Community Slack** (optional): the worker invites newly approved mentors
  to the Slack workspace via `admin.users.invite` (`SLACK_ADMIN_TOKEN`,
  `SLACK_TEAM_ID`, `SLACK_INVITE_CHANNEL_IDS` — Enterprise Grid only;
  leave the token empty to disable).
- **Analytics** via PostHog (`ANALYTICS_PROVIDER`: `none` | `posthog`).
- **Observability**: OTLP traces + Prometheus metrics + JSON logs shipped by
  a Grafana Alloy sidecar container (`O11Y_*`), optional Pyroscope
  continuous profiling (`O11Y_PROFILING_*`).

## Local development

```bash
# 1. Postgres in Docker
docker run -d --name openmentor-pg -p 5432:5432 \
  -e POSTGRES_USER=openmentor -e POSTGRES_PASSWORD=password \
  -e POSTGRES_DB=openmentor postgres:16-alpine

# 2. Configuration
cp .env.example .env   # fill in DATABASE_URL, tokens, etc.

# 3. Migrations, API, worker
make migrate           # builds bin/migrate and runs scripts/migrate.sh
make run               # API on :8081
make run-worker        # worker on :8090
```

Other useful targets: `make build` (all three binaries), `make test`,
`make test-race`, `make lint`, `make ci` (fmt-check + vet + lint +
test-race + gosec), `make docker-build`. The full dev stack (frontend +
api + worker + postgres) lives in `openmentor-infra/docker-compose.dev.yml`.

## Testing & CI

```bash
go test ./...          # unit tests (also under test/ mirroring the tree)
make ci                # everything the PR gates run
```

GitHub Actions:
- **build-and-test.yml** (push/PR to `main`): go vet, tests with coverage,
  builds all three binaries, then builds the Docker image and smoke-tests
  migrate → api → worker against a real Postgres container.
- **pr-checks.yml** (PRs): tests with race detector + coverage threshold,
  gosec security scan, cross-platform build check, gofmt/vet/staticcheck.

## Deployment

Deployed via [`infra/`](../infra/) (docker-compose behind Traefik): the
`migrate` service runs first, then `backend` (API) and `worker` start from
the same image. Only the frontend is publicly exposed;
API and worker stay on the internal network. See `infra/` for compose
files, environment reference, and deploy scripts.

## License

See [LICENSE](LICENSE).
