# openmentor.io

An open community of tech mentors — mentees find a mentor, send a request, and meet one-on-one. The platform connects people and stays out of the way: no payment processing, no lock-in, sessions are arranged directly between mentor and mentee.

OpenMentor is a community project: donation-funded, zero commission, no ads — and mentoring for free is encouraged. The full story is at [openmentor.io/about](https://openmentor.io/about).

This is the monorepo for the whole platform.

## Layout

| Directory | What it is | Stack |
|---|---|---|
| [`web/`](web/) | The website: mentor catalog, profiles, contact flow, mentor dashboard, admin moderation | Next.js 16, TypeScript, Tailwind |
| [`api/`](api/) | Backend: REST API, background worker (email + cron jobs), DB migrations — three binaries, one Docker image | Go, PostgreSQL |
| [`infra/`](infra/) | Deployment: Docker Compose + Traefik on a single VM, Postgres with backups, Grafana Alloy observability | Compose, shell |
| [`docs/`](docs/) | Decisions log, runbooks, legal drafts, design reference, historical migration plan | Markdown |
| [`brand/`](brand/) | "Fresh Signal" brand asset pack — logos, color tokens (source of truth for `web/public/brand`) | SVG/PNG/CSS |

Each component keeps its own README with details; start there.

## Architecture in one paragraph

The web app is a thin client — every data operation goes through the Go API (mentors catalog, magic-link auth for mentors and moderators, request lifecycle). Side effects (transactional email via AWS SES, daily reminder/deactivation crons) run in a separate `worker` process built from the same Go codebase — isolated crash domain, own DB pool, talked to via authenticated internal HTTP triggers. Everything deploys as one Compose stack behind Traefik on a single VM, with Postgres in a container (protected external volume + nightly dumps to S3), and metrics/logs/traces/profiles shipped to Grafana Cloud through Alloy.

## Local development

```bash
# 1. Database
docker run -d --name openmentor-pg \
  -e POSTGRES_USER=openmentor -e POSTGRES_PASSWORD=openmentor -e POSTGRES_DB=openmentor \
  -p 5432:5432 postgres:16-alpine

# 2. API (see api/.env.example for required vars)
cd api && cp .env.example .env   # fill in dev values
go run ./cmd/migrate && go run ./cmd/api   # and optionally: go run ./cmd/worker

# 3. Web (see web/.env.example; GO_API_INTERNAL_TOKEN must match the API's INTERNAL_MENTORS_API)
cd web && cp .env.example .env && yarn install && yarn dev
```

Full-stack via Compose: see [`infra/README.md`](infra/README.md).

## CI

| Workflow | Trigger | Purpose |
|---|---|---|
| `Checks` | every PR | The one **required** branch-protection check (`Checks / required-checks`) — runs quick gates for whatever changed, passes trivially otherwise |
| `CI / Web` | changes under `web/` | lint, typecheck, tests, production build |
| `CI / API` | changes under `api/` | race tests + coverage floor, gofmt/staticcheck, gosec, full Docker smoke test (postgres → migrate → api → worker) |
| `Deploy` | manual dispatch | builds both images from one SHA, ships the stack to the VM with health-checked rollback |

## History

openmentor.io began as a fork of [getmentor.dev](https://getmentor.dev) — a Russian-language mentorship community by the same author — adapted for a global audience: translated, redesigned, Telegram-free, and consolidated from five repositories into this monorepo (fresh history; the component repos remain as archives). The full story lives in [`docs/migration/`](docs/migration/).

## License

[AGPL-3.0](LICENSE) for the entire repository.
