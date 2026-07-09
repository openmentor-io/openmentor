# openmentor.io — frontend

[OpenMentor](https://openmentor.io) is an open community mentorship marketplace: mentees browse a catalog of tech mentors, filter by expertise, price, and experience, and book one-on-one sessions directly. Mentors manage their profile, visibility, and incoming requests through a web portal.

This directory is the web frontend of the openmentor monorepo. It is a **thin client**: all data operations (PostgreSQL, object storage, email, auth tokens, Turnstile verification) are handled by the Go backend in [`../api`](../api). Next.js API routes only proxy to it.

## Stack

- [Next.js 16](https://nextjs.org/) (Pages Router) + TypeScript (strict mode)
- Tailwind CSS with the "Fresh Signal" brand tokens (`src/styles/brand-tokens.css`, assets in `public/brand/`)
- Jest + React Testing Library
- Observability: Winston logging, Prometheus metrics, OpenTelemetry server tracing, Grafana Faro (client), PostHog error tracking/analytics

## Architecture

```
Browser → Next.js pages / API routes → Go API (../api) → PostgreSQL / S3 / email
```

Key areas:

- `/` — mentor catalog with search and filters
- `/mentor/[slug]` — mentor profile and contact flow
- `/bementor` — mentor registration
- `/mentor/*` — mentor portal (email-link login, requests inbox, profile editing, visibility toggle)
- `/admin/*` — moderation portal (approve/decline mentor applications)
- `src/pages/api/*` — proxy routes to the Go API (contact, registration, mentor/admin auth and profile, reviews, healthcheck, metrics)

## Local development

Prerequisites: Node 22.x, yarn, and a running Go API instance from [`../api`](../api) (defaults to `http://localhost:8081`).

```bash
# 1. Configure environment
cp .env.example .env    # then fill in values; see comments in the file

# 2. Install dependencies
yarn install

# 3. Start the Go API (in ../api), then the frontend
yarn dev                # http://localhost:3000
```

The minimum useful configuration is `NEXT_PUBLIC_GO_API_URL` and `GO_API_INTERNAL_TOKEN`; storage, analytics, and observability vars are optional for local work.

## Testing & checks

```bash
yarn lint               # ESLint (src/)
npx tsc --noEmit        # Type check
yarn test               # Jest test suite
yarn build              # Production build
```

All four run in CI (`.github/workflows/main.yml`) on pushes and PRs to `main`.

## Deployment

The app ships as a standalone-output Docker image (see `Dockerfile`; `./docker-build-test.sh` builds it locally from your `.env`). Deployment — Docker Compose + Traefik, environment injection, and the Go API — is managed in [`../infra`](../infra).

## License

[AGPL-3.0](LICENSE). Forked from [getmentor.dev](https://getmentor.dev).
