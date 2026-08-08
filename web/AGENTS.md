# AGENTS.md — `web/`

Instructions for any coding agent working in `web/`, the Next.js frontend of the openmentor
monorepo. **Read the repo-root `AGENTS.md` too** — it holds the repo-wide rules (CI gate shape,
monorepo commit rules, decision log, testing bar). Under the AGENTS.md convention the nearest
file wins and does *not* merge with the root one, so the rules below that also appear there are
repeated deliberately.

## What this is

A TypeScript Next.js 16 app that is a **BFF, not a data layer**: every read and write goes
through `src/pages/api/*` proxies to the Go API in `../api`, which owns Postgres, S3 and email.
It is **live in production**. Full stack locally: `../infra`.

## Commands

```bash
make ci          # eslint + tsc --noEmit + jest + production build — run before opening a PR
make lint        # eslint over the package
make typecheck   # npx tsc --noEmit
make test        # jest
npm run dev      # dev server on http://localhost:3000
./docker-build-test.sh   # build the image with .env, then: docker run -p 3000:3000 --env-file .env openmentor:multi-stage-test
```

CI calls these same targets. Don't inline a different command in a workflow.

## Pages Router only

There is no `app/` directory and no plan for one. Pages live in `src/pages/`, API routes in
`src/pages/api/`. An answer written for the App Router (`route.ts`, server components,
`app/layout.tsx`, `next/headers`) will not compile here.

## Metric route labels

`src/lib/with-observability.ts` owns the `http_route` label on three Prometheus series
(`httpRequestTotal`, `httpRequestDuration`, `activeRequests`). Two mechanisms, both load-bearing:

- `normalizeRoute()` collapses UUID segments to `:id` at any depth, because one id-bearing
  request otherwise mints three new series.
- `KNOWN_ROUTES` is a hard-coded allowlist of route templates; `routeLabel()` maps anything else
  to `other`. That caps cardinality at `len(KNOWN_ROUTES) + 1` — necessary because these metrics
  are labelled *before* the handler authenticates anything, and a non-UUID id (an Airtable
  `rec…`, a numeric id) survives normalization verbatim.

**Never build a label from `req.url` yourself — always go through `routeLabel()`.** Wrap every
new API route in `withObservability(handler)` and add its template to `KNOWN_ROUTES`;
`src/lib/__tests__/with-observability.test.ts` walks `src/pages/api` for files containing
`withObservability(`, derives each template from its path, and asserts `routeLabel` returns that
template — so a forgotten entry fails CI. These label values are **live Grafana dashboard
dimensions** (`grafana/dashboards/om-frontend.json` queries `http_route` by name), so renaming
one changes the panels' series.

## Redaction and PII

`src/lib/redact.ts` is the single place capability-bearing values are stripped. It feeds the
PostHog `before_send` hook (`src/lib/posthog.ts`) and the Faro `beforeSend` hook
(`src/lib/faro.ts`). **Extend it rather than adding a second masker** — an exact-key list is
exactly what let `login_token`, `confirm_token` and `request_id` through before.

Never let a UUID, a capability token or PII reach a log, a URL, a span attribute or an analytics
property. A review `request_id` and a magic-link `token` are bearer credentials that travel in
URLs. `rvw_`-prefixed review tokens need a *shape* rule as well as a key rule, because session
replay serializes DOM attribute values verbatim. The Go side mirrors these rules in
`api/pkg/redact`; the two cannot import each other, so `src/lib/__tests__/telemetry-redaction.test.ts`
pins this copy's shape.

## Images and page payload

- **Image optimization is off for good** (`images.unoptimized: true`, decision D40). Photos are
  fetched straight from the CDN by `src/lib/image-loader.ts`. It is set config-level, not as 18
  per-usage `unoptimized` props, so a new `<Image>` that forgets the prop cannot re-arm the
  optimizer. `sharp` and `images.remotePatterns` are gone — with the optimizer off nothing is
  ever proxied, so there is nothing to allowlist. **Don't reintroduce any of the three.**
- `experimental.largePageDataBytes` is deliberately raised. Prefer projecting the fields a page
  actually renders over shipping full mentor rows into `__NEXT_DATA__` — the homepage calls
  `getAllMentors({ onlyVisible: true, drop_long_fields: true })` for that reason.

## TypeScript conventions

- Strict mode. Explicit parameter types, handle null/undefined, no `any` (use `unknown`).
- `.ts` for non-UI modules, `.tsx` for components and pages. PascalCase component filenames,
  `useX` for hooks.
- Import via `@/…` aliases, never `../../`. Use `import type` for type-only imports.
- Domain types live in `src/types/` (`MentorBase`, `MentorWithSecureFields`, `MentorListItem`,
  `CalendarType`, `ExperienceLevel`) and are re-exported from barrels — read the barrel before
  inventing a type.

## Layout

| Path | What lives there |
|---|---|
| `src/pages/`, `src/pages/api/` | Pages and the BFF proxy routes |
| `src/components/` | `ui/`, `forms/`, `layout/`, `mentors/`, `mentor-admin/`, `admin-moderation/`, `calendar/`, `hooks/` |
| `src/lib/` | Go API client, logger, metrics, redaction, analytics, tracing |
| `src/server/` | Server-side data access (`mentors-data.ts`) |
| `src/types/`, `src/config/`, `src/styles/`, `public/` | Types, filters/SEO config, Tailwind styles, assets |
| `src/__tests__/`, `src/lib/__tests__/`, `src/components/hooks/__tests__/` | Tests |

## Design system

All UI work follows the 2026-07 redesign. **Read `../docs/design-reference/design-system.md`
before touching an interface**; the authoritative mockups are
`../docs/design-reference/redesign/*.dc.html`. Hard rules: no Tailwind `gray-*` (ink/surface/line
family only), one button system (the `.button*` classes), radii/shadows/pastels only through the
tokens in `tailwind.config.js`.

## Environment variables

`.env.example` is the contract; `src/types/env.d.ts` declares the same set and must stay in sync
with it, and with `infra/.env.example` / `infra/.env.production.example` — a var that exists in
one and not the others reaches no container. Read them rather than guessing a name.

## Testing

Jest + `@testing-library/react` (jsdom) for components, `node-mocks-http` for API route handlers.
Name tests `*.test.ts(x)` and put them in the existing `__tests__` structure. Mock the Go API
client and Turnstile; wrap async state updates in `act()`. Every fix needs a test verified to fail
without it.

## Formatting and commits

**Prettier disagrees with committed formatting in 131 files** (`npx prettier --check 'src/**/*.{ts,tsx}'`).
`lint-staged` runs `prettier --write` on staged `*.ts(x)` via the `simple-git-hooks` pre-commit
hook, so touching one line of such a file can reformat large untouched regions and bury the
semantic change. Keep commits to semantic changes; if a reformat lands, it belongs in its own
commit.

Node **26.x** is required (`package.json` `engines`); the Dockerfile pins
`node:26.5.1-alpine3.23` in all three stages. Bump `engines`, the Dockerfile stages and the CI
`node-version` pins together.

Feature branches only — never merge to `main` without explicit permission. A change that also
touches the API contract, an env var or a compose service lands as one PR across `web/`, `api/`
and `infra/`.
