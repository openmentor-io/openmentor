# AGENTS.md

Instructions for any coding agent working in the openmentor monorepo. Agent-agnostic;
`CLAUDE.md` imports this file and adds Claude Code specifics.

**This is the single instruction file for the repo.** There are no per-directory `AGENTS.md` or
`CLAUDE.md` files — don't go looking for one. Everything that governs `api/`, `web/`, `infra/`
and `grafana/` is in the sections below. It is long on purpose: `AGENTS.md` has no import
mechanism, so a split file is invisible to any agent that starts at the repo root.

## What this is

An open-source mentorship platform (AGPL-3.0), a fork of getmentor.dev, **live in production**
with real user data on a single VM. Short planned downtime is acceptable; extended downtime is
not. Treat every change as reaching real mentors and mentees.

Request path: browser → Traefik (the only service publishing :80/:443) → Next.js frontend →
`/api/*` BFF proxy → Go API → Postgres. The worker is internal-only, driven by cron and by
fire-and-forget HTTP calls from the API.

| Directory | What it is |
|---|---|
| `web/` | Next.js 16 frontend, **Pages Router only**. Acts as a BFF: all data flows through `web/src/pages/api/*` proxies to the Go API. |
| `api/` | Go backend, module `github.com/openmentor-io/openmentor/api`. Three binaries: `cmd/api`, `cmd/worker`, `cmd/migrate`. |
| `infra/` | Docker Compose + Traefik single-VM deployment, Grafana Alloy, Postgres backup sidecar. Scripts here run as root on that VM. |
| `grafana/` | Dashboards and alert rules as code. `dashboards/` is Git-Synced; `alerting/` is **not**. |
| `docs/` | `docs/migration/DECISIONS.md` (decision log), `docs/runbooks/` (operator procedures), `docs/audit/` (the 2026-08 audit record), `docs/design-reference/` (design system + mockups). |
| `brand/` | Brand asset pack. Never redraw the logo; reference files verbatim per `brand/README.md`. Served copies live in `web/public/brand`. |

## Commands

Each subtree owns its verification. Run the `make` target, not the underlying tool — the CI
workflows call these same targets, so local and CI run the identical check.

```bash
cd api   && make ci      # lint (golangci-lint, version pinned in api/Makefile) + test-race
cd web   && make ci      # eslint + tsc --noEmit + jest + production build
cd infra && make check   # 11 suites: compose-config, env-allowlist, backup-tests,
                         # deploy-tests, rollback-tests, alert-tests, alert-fireability-tests,
                         # alloy-redaction-tests, advisory-lock-tests, metrics-keeplist-tests,
                         # migration-mapper-tests (needs node)
                         # (shellcheck and db-identity-tests are separate targets, NOT in check)
```

Never inline a command in a workflow that differs from its make target. That divergence is how
CI once went green while `make lint` reported 44 issues. The narrower targets are listed in the
`api/`, `web/` and `infra/` sections below.

## Repo-wide rules

- **Cross-cutting changes land as ONE commit/PR** touching every affected directory — API
  contract, env vars, compose services. That is the point of the monorepo.
- **Every new feature gets its own branch. Never merge to `main` without explicit permission.**
- **Product and architecture decisions get a row in `docs/migration/DECISIONS.md`.** When
  several branches run in parallel, reserve an id block per branch — four branches once all
  claimed `D39` and reconciling it cost real effort.
- **Never commit a real `.env` or a secret.** Templates are `*.example`; the root `.gitignore`
  enforces this, so don't weaken it.
- **The repository is public.** Don't commit a live capability value (a `client_requests.id`, a
  login or review token), and don't add exploitation detail for a vulnerability that is not
  fixed yet.
- `docs/migration/` is a historical record of the getmentor→openmentor fork. Don't "fix" old
  paths there. Same for `docs/audit/`: it records what an audit claimed, and §4 lists the claims
  later verification overturned — read that section before acting on the plan.
- **Keep this file current.** When you learn something a future agent would otherwise rediscover
  the hard way — a non-obvious constraint, a gotcha that cost you a debugging session, a
  convention you had to be corrected on — add it here in the same PR, with the failure it
  prevents. A rule without its reason gets "simplified" away later. Things that belong here:
  invariants, gotchas, and conventions that differ from tool defaults. Things that do not:
  directory listings, dependency lists and anything else derivable from the codebase — those go
  stale silently and are what rotted the file this one replaced.
- **Keep code comments short, and omit them when the change is trivial.** Explain *why* — a
  constraint the code cannot state itself. Never narrate what the next line does, restate the
  diff, or record how a bug used to behave; that belongs in the commit message or a decision
  row, and reads as noise the moment the PR merges.

## Migrations

`api/migrations/`, applied by `cmd/migrate` (golang-migrate).

- **Line 1 declares the phase**: `-- phase: expand` (only adds — tables, columns, indexes, seed
  rows, widened constraints) or `-- phase: contract` (removes, renames, or narrows, so an older
  image can no longer find what it reads). `infra/rollback.sh` reads these markers out of the
  deployed image to decide whether a rollback may cross a migration boundary, so an unmarked or
  mismarked file silently degrades a production safety check. `cd infra && make check` fails on
  both, and on a missing `.down.sql`.
- **A narrowing `CHECK` is `contract`, and no automated check will tell you.** The tripwire in
  `rollback-migration-guard-test.sh` greps for `DROP`/`RENAME`/`DELETE FROM`/`SET NOT NULL`, none
  of which appear in `ADD CONSTRAINT … CHECK` — yet a CHECK on an existing column stops an older
  image from writing values it still produces, which is exactly what the phase marker exists to
  record. It cannot be added to the tripwire either: a CHECK that *widens* an enum (`000005`) or
  that lands on a brand-new column (`000006`) is correctly `expand`, so the regexp would be wrong
  more often than right. Pick the marker by asking whether the previous image can still write.
- **A new `CHECK` on a `mentors` column needs an override in `dbtest.FillNullable`.** The nullable
  -column test fills every column from a generic per-type filler, so a constraint the filler cannot
  satisfy fails the seed INSERT and every subtest then fails blaming its own column — a wall of
  unrelated failures with no mention of the constraint. `mentor_nullable_columns_db_test.go` passes
  `price` for this reason; `mentor_update_db_test.go` needs the same for its allowlist fixture.
- **Every migration ships a `.down.sql`, and it documents what it cannot restore.** Down-migrations
  are for a human to run deliberately: `000009_modernise_tags.down.sql` cannot bring back the
  mentor–tag associations its up-migration cascaded away. **Never wire a lossy down-migration into
  anything automatic.**
- **A contract migration ships a release *after* the code that stopped reading the old shape.**
  expand → deploy → contract. `migrate` shares `BACKEND_IMAGE_TAG` with backend and worker, so the
  schema and the image move together or the deploy gate fails half-way.
- **Run `make migration-check` before you push a migration.** Two branches that both add
  `000014_*.sql` are *different files*: git merges them with no conflict, and golang-migrate then
  records one version number for both, so the second is never applied and never reported pending.
  Worse, `Up()` only walks **forward** from the recorded version, so a migration merged below what
  production already applied is never applied *there* while every fresh database and every CI run
  gets it — invisible until a query hits the missing column in production. The script fails on
  duplicate versions, a missing `.up.sql`/`.down.sql` counterpart, a gap in the sequence, and a
  version not greater than the base ref's highest.

## Database writes

- **A write that gates a side effect must require exactly one affected row.** The house pattern is
  in `api/internal/worker/repository.go`: the method returns `(applied bool, err error)` from
  `tag.RowsAffected() > 0`, and the caller sends the email / fires the webhook only when `applied`
  is true. Without it, an overlapping cron pass or a redelivered POST emails the mentor twice.
- **Compare-and-set on the value you read, not just on a status.** `UPDATE … SET status='draft'
  WHERE status='draft'` is *not* exclusive: Postgres re-evaluates the WHERE against the winner's
  committed row, which still matches, so the second caller wins too and overwrites the token the
  first one already emailed. `FinalizeNewMentor` closes it by also matching the confirmation token
  it read (`COALESCE(email_confirmation_token,'') = $9`) and by refusing to touch a token whose
  window is still open.
- **A row and its dependent rows go in one transaction.** `CreateMentor` and `UpdateMentorTags`
  (`api/internal/repository/mentor_repository.go`) both hold `pool.Begin` across the row write and
  the `mentor_tags` rewrite, so a mentor never exists with half its tags.
- **Prove these against a real Postgres with concurrent callers.** Transaction, single-affected-row
  and single-use guarantees are SQL properties; a mock proves nothing about them. See the
  `*_db_test.go` files (`api/internal/worker/repository_claim_db_test.go`,
  `api/internal/repository/review_invitation_db_test.go`,
  `api/internal/repository/session_consume_db_test.go`).

## Logging, PII and capabilities

Both sides of the stack; the mechanics differ per language (Go: `api/pkg/redact`; TypeScript:
`web/src/lib/redact.ts`, detailed in the frontend section).

- **Never log an email, a name, a contact detail or request text.** Not even hashed:
  low-entropy PII must be *masked*, not hashed — a hashed street address in Loki is a membership
  oracle for anyone who can guess addresses.
- **Never let a UUID, a capability token or PII reach a log, a URL, a span attribute or an
  analytics property.** `client_requests.id`, login tokens, email-confirmation tokens and review
  tokens are bearer credentials, and they already travel in URLs. Use `redact.ID`
  (`api/pkg/redact`) when an identifier has to appear at all.
- **`logger.RedactedError(err)` is the house style, not `zap.Error(err)`.** Error strings carry the
  row ids and payloads the repository layer put in them; `api/internal/worker` contains zero
  `zap.Error` in non-test code for exactly that reason.

## Go backend (`api/`)

Module `github.com/openmentor-io/openmentor/api`, Go 1.26. Three binaries from one image:

| Binary | Role |
|---|---|
| `cmd/api` | Gin HTTP API. The only one the frontend talks to. |
| `cmd/worker` | Internal-only. Cron plus fire-and-forget HTTP calls from the API. |
| `cmd/migrate` | Runs golang-migrate at deploy time, gated by `service_completed_successfully`. |

### Commands

```bash
make ci               # lint + test-race — what the PR gate runs
make lint             # golangci-lint, version pinned by GOLANGCI_VERSION in api/Makefile
make test             # go test ./...
make migration-check  # migration NUMBERING guard; needs no database
make migration-test   # every migration up → down → up against a throwaway Postgres (needs docker)
make install-tools    # installs the pinned golangci-lint into GOPATH/bin
```

`make lint` refuses any `golangci-lint` whose version isn't `GOLANGCI_VERSION` or that was built
with an older Go than `go.mod` targets — a mismatched binary reports a different set of issues than
CI. `golangci-lint` already covers gofmt, `go vet`, staticcheck and gosec, so `ci` does not invoke
them again.

### The nullable-column rule

`mentorSelect` (`api/internal/repository/mentor_repository.go`) is the one column list every full
mentor read shares. **Every nullable column added there must either be COALESCEd in the query or
land in a pointer field of `models.Mentor`** — pgx fails the *whole row scan* on a NULL into a
non-pointer destination, so one un-COALESCEd column takes out every caller at once. It has: one
column broke login, the public profile page and the catalog together. `airtable_id` is the only
legitimate pointer (nil means "registered natively").
`api/internal/repository/mentor_nullable_columns_db_test.go` enumerates the nullable columns from
`information_schema` and checks each one against a real database — **except GENERATED columns**
(`mentors.price_amount`), which it must skip because they cannot be written: the enumeration
works by filling and blanking each column, and Postgres refuses an UPDATE on a generated one. So
a generated column pulled into a mentor SELECT is under the same COALESCE-or-pointer rule with
NO test catching a miss — the reviewer is the enforcement. The same rule applies to
`client_requests`, which is nullable nearly everywhere.

### Reuse, don't reinvent

| Package | Use it for, and the failure it prevents |
|---|---|
| `pkg/safego` | `safego.Go(task, fn)` instead of a bare `go func()` on any request path. `recover()` is per-goroutine, so Gin's recovery middleware cannot catch a panic in a goroutine it spawned — one such panic killed the process *after* the handler had already returned 200. |
| `pkg/redact` | Query, path, URL, free-text and id redaction. One implementation, so a new sink can't get a weaker one. |
| `pkg/tracing/redact.go` | Strips capabilities from span attributes by wrapping the OTLP **exporter**, not by registering a SpanProcessor: `otelhttp` sets its attributes *after* `tracer.Start`, so a processor runs too early to see them. |
| `pkg/imageclass` | Bounded image decode. `MaxDecodeBytes × maxConcurrentDecodes` is a deliberate share of the API container's `mem_limit` (512m in `infra/docker-compose.yml`), pinned by `TestDecodeBudgetFitsContainer`. Changing a constant means re-deciding that budget. |
| `internal/middleware/admission.go` | Bounds requests **in flight** (not arrival rate) on the big-body endpoints; the slot is taken before the body is read. It is the other half of the same memory budget. |
| `test/dbtest` | DB-backed tests. It takes a session-level Postgres advisory lock because the migrations are not all idempotent, and it **skips** (`t.Skipf`) when `OPENMENTOR_TEST_DATABASE_URL` is unset — so read the `--- SKIP` lines before believing a `*_db_test.go` passed. |

### Configuration

Config validation is **per binary**: `ValidateForAPI`, `ValidateForWorker`, `ValidateForMigrate`
in `api/config/config.go`, all fail-fast at startup. **Don't move a requirement one binary needs
into the shared path.** Doing that once forced S3 credentials into `migrate` and `worker`, which
then had to be widened in `infra/env-allowlist.txt` — handing two containers a secret neither
reads. The Docker smoke test in `.github/workflows/ci-api.yml` deliberately uses a separate env
file per binary (`ci-migrate.env`, `ci-api.env`, `ci-worker.env`); a single shared file would only
ever prove the union validates.

Any env-var change lands as one PR across `api/config/config.go`, `web/.env.example`,
`web/src/types/env.d.ts`, `infra/.env.example`, `infra/.env.production.example`,
`infra/docker-compose.yml` and `infra/env-allowlist.txt`.

## Frontend (`web/`)

A TypeScript Next.js 16 app that is a **BFF, not a data layer**: every read and write goes
through `src/pages/api/*` proxies to the Go API in `../api`, which owns Postgres, S3 and email.
Full stack locally: `../infra`.

### Commands

```bash
make ci          # eslint + tsc --noEmit + jest + production build — run before opening a PR
make lint        # eslint over the package
make typecheck   # npx tsc --noEmit
make test        # jest
npm run dev      # dev server on http://localhost:3000
./docker-build-test.sh   # build the image with .env, then: docker run -p 3000:3000 --env-file .env openmentor:multi-stage-test
```

### Pages Router only

There is no `app/` directory and no plan for one. Pages live in `src/pages/`, API routes in
`src/pages/api/`. An answer written for the App Router (`route.ts`, server components,
`app/layout.tsx`, `next/headers`) will not compile here.

### Metric route labels

`src/lib/with-observability.ts` owns the `http_route` label on three Prometheus series
(`httpRequestTotal`, `httpRequestDuration`, `activeRequests`). The label is a **compile-time
literal**: `withObservability('/api/mentor/example/:id', handler)`, typed as `ApiRouteLabel` — a
union derived from the `API_ROUTE_LABELS` tuple in `src/lib/api-routes.ts`. `req.url` has no path
to the label at all, so cardinality equals the number of call sites; the old
`normalizeRoute()`/`KNOWN_ROUTES` allowlist is gone (C7, D76) — don't reintroduce a runtime
mapping.

Adding an API route touches two places, in one edit pass: the tuple in `src/lib/api-routes.ts`
(every dynamic segment spelled `:id`) and the handler's `withObservability('<template>', handler)`
wrapper. Three guards enforce it: the `ApiRouteLabel` type (a missing or undeclared label fails
`tsc`), a dev-only import-time assertion (no-op in production — a labelling bug must not take a
live endpoint down), and `src/lib/__tests__/api-routes.test.ts`, which parses every file under
`src/pages/api` and asserts the literal each one passes equals its own path-derived template —
the one mistake the type cannot see is a *valid* label belonging to a sibling route.
`/api/metrics` is the sole deliberately unwrapped handler (a scrape must not write to the
registry it is serializing), and that exemption is asserted too. These label values are **live
Grafana dashboard dimensions** (`grafana/dashboards/om-frontend.json` queries `http_route` by
name), so renaming one changes the panels' series.

### Redaction

`src/lib/redact.ts` is the single place capability-bearing values are stripped. It feeds the
PostHog `before_send` hook (`src/lib/posthog.ts`) and the Faro `beforeSend` hook
(`src/lib/faro.ts`). **Extend it rather than adding a second masker** — an exact-key list is
exactly what let `login_token`, `confirm_token` and `request_id` through before. `rvw_`-prefixed
review tokens need a *shape* rule as well as a key rule, because session replay serializes DOM
attribute values verbatim. The Go side mirrors these rules in `api/pkg/redact`; the two cannot
import each other, so `src/lib/__tests__/telemetry-redaction.test.ts` pins this copy's shape.

### Images and page payload

- **Image optimization is off for good** (`images.unoptimized: true`, decision D40). Photos are
  fetched straight from the CDN by `src/lib/image-loader.ts`. It is set config-level, not as 18
  per-usage `unoptimized` props, so a new `<Image>` that forgets the prop cannot re-arm the
  optimizer. `sharp` and `images.remotePatterns` are gone — with the optimizer off nothing is
  ever proxied, so there is nothing to allowlist. **Don't reintroduce any of the three.**
- `experimental.largePageDataBytes` is deliberately raised. Prefer projecting the fields a page
  actually renders over shipping full mentor rows into `__NEXT_DATA__` — `src/pages/index.tsx`
  calls `getAllMentors({ onlyVisible: true, drop_long_fields: true })` for that reason.

### TypeScript conventions

- Strict mode. Explicit parameter types, handle null/undefined, no `any` (use `unknown`).
- `.ts` for non-UI modules, `.tsx` for components and pages. PascalCase component filenames,
  `useX` for hooks.
- Import via `@/…` aliases, never `../../`. Use `import type` for type-only imports.
- Domain types live in `src/types/` (`MentorBase`, `MentorWithSecureFields`, `MentorListItem`,
  `CalendarType`, `ExperienceLevel`) and are re-exported from barrels — read the barrel before
  inventing a type.

### Layout

`src/pages/` + `src/pages/api/` (pages and BFF proxy routes), `src/components/` (`ui/`, `forms/`,
`layout/`, `mentors/`, `mentor-admin/`, `admin-moderation/`, `calendar/`, `hooks/`), `src/lib/`
(Go API client, logger, metrics, redaction, analytics, tracing), `src/server/` (server-side data
access, `mentors-data.ts`), plus `src/types/`, `src/config/`, `src/styles/`, `public/`. Tests sit
in `__tests__/` beside what they cover.

### Design system

All UI work follows the 2026-07 redesign. **Read `docs/design-reference/design-system.md`
before touching an interface**; the authoritative mockups are
`docs/design-reference/redesign/*.dc.html`. Hard rules: no Tailwind `gray-*` (ink/surface/line
family only), one button system (the `.button*` classes), radii/shadows/pastels only through the
tokens in `web/tailwind.config.js`.

### Environment variables

`web/.env.example` is the contract; `src/types/env.d.ts` declares the same set and must stay in
sync with it, and with `infra/.env.example` / `infra/.env.production.example` — a var that exists
in one and not the others reaches no container. Read them rather than guessing a name.

### Formatting and Node version

**Prettier disagrees with committed formatting in 131 files** (`npx prettier --check 'src/**/*.{ts,tsx}'`).
`lint-staged` runs `prettier --write` on staged `*.ts(x)` via the `simple-git-hooks` pre-commit
hook, so touching one line of such a file can reformat large untouched regions and bury the
semantic change. **Never run `prettier --write` across the package**, and don't "fix formatting
while you're in there" — a broad write produces a diff nobody can review. If a reformat lands, it
belongs in its own commit.

Node **26.x** is required (`package.json` `engines`); the Dockerfile pins
`node:26.5.1-alpine3.23` in all three stages. Bump `engines`, the Dockerfile stages and the CI
`node-version` pins together.

### Web testing

Jest + `@testing-library/react` (jsdom) for components, `node-mocks-http` for API route handlers.
Name tests `*.test.ts(x)` and put them in the existing `__tests__` structure. Mock the Go API
client and Turnstile; wrap async state updates in `act()`. `make ci` type-checks and renders
nothing — verify UI work in a browser (`npm run dev`) and say what you looked at. The Wysiwyg
toolbar regression that shipped under a green suite came from a test that mocked the component
wholesale.

## Infrastructure and deploy (`infra/`)

Docker Compose behind Traefik on a single production VM, a Postgres backup sidecar, and Grafana
Alloy. Scripts here run as root on that VM. A quoting slip is an incident, not a stack trace.

### `make check` — eleven suites

| Target | What it proves |
|---|---|
| `compose-config` | `docker-compose.yml` renders, and so does the prod + `docker-compose.dev.yml` overlay |
| `env-allowlist` | `check-service-env.sh --self-test` — per-service env allowlist *and* secret ownership, plus that the check still bites |
| `backup-tests` | `postgres-backup/backup-test.sh` — sidecar behaviour |
| `deploy-tests` | `deploy-transition-test.sh` — the blocks `deploy-remote.sh` and `rollback.sh` share |
| `rollback-tests` | `rollback-migration-guard-test.sh` — the migration-boundary guard and the expand/contract policy, reading `../api/migrations` |
| `alert-tests` | `alert-consistency-test.sh` — `../grafana/alerting` and `../grafana/dashboards` against `docker-compose.yml` |
| `alert-fireability-tests` | `alert-fireability-test.sh` — every rule's labels exist and its threshold is reachable |
| `alloy-redaction-tests` | `alloy-redaction-test.sh` — the PII redaction stage rewrites what it must and nothing else, and `alloy validate` passes |
| `advisory-lock-tests` | `advisory-lock-namespace-test.sh` — the Postgres advisory-lock namespaces across Go and the migration script do not collide |
| `metrics-keeplist-tests` | `metrics-keeplist-test.sh` — every series `../grafana` reads survives the Alloy relabel keep-lists, and the filters still bite |
| `migration-mapper-tests` | `migration/mapprice.test.js` (`node --test`) — every value `mapPrice` can emit satisfies `mentors_price_chk`, so a mentor import cannot abort mid-run on the constraint |

Not in `check`, run them when relevant: `make shellcheck`, `make db-identity-tests`.

`ENV_FILE` falls back to `.env.example` when the machine has no `.env`. Only key names and
structure are ever read, so placeholder values are fine, nothing is copied over a real `.env`,
and `make check` is safe to run without credentials.

### No shared env file

**There is no `env_file:` anywhere in the compose files, and there must not be.** A shared file
gives every container every secret — the internet-facing frontend would hold `DATABASE_URL`. Each
service declares an explicit `environment:` list, and `check-service-env.sh` asserts, per service,
that every key it declares appears in that service's section of `env-allowlist.txt`, and that the
keys in its `SECRET_OWNERS` table appear *only* in the services entitled to them.

Adding an env var to a service therefore means adding it to `env-allowlist.txt` as well, in that
service's section, or `make check` fails.

**The trap:** a bare `- KEY` entry (no `=value`) is dropped by compose when it cannot resolve the
key, so a variable that exists only in the production environment would render away — passing both
checks while reaching no container. `check-service-env.sh` parses the compose file for *declared*
keys and seeds every bare entry before rendering, precisely so that cannot hide. Don't defeat that
by "simplifying" the declared-keys pass.

### Deploy and rollback

Never run `deploy.sh`, `rollback.sh`, `deploy-remote.sh` or `db.sh` yourself, and never apply an
alert rule group. Propose the command and let the operator run it — reading a script is fine,
executing one reaches production.

- `./deploy.sh` defaults to targets **`frontend backend`** — it does **not** sync `infra/`. A
  compose, allowlist or Alloy change reaches the VM only via `./deploy.sh infra` (or `all`).
  Merging a compose change and running a normal deploy changes nothing on the VM.
- **Migrations apply themselves.** The `migrate` service runs from the same image as backend and
  worker, and `backend`/`worker` depend on it with `condition: service_completed_successfully`. A
  migration that fails to validate its config takes the whole deploy down — that is what happened
  on 2026-08-04.
- **The rollback target is `.env.lastgood`, not `.env.backup`.** `.env.lastgood` is written only
  *after* health checks pass, so it always names a version known good. The `.env.backup.<epoch>`
  files are timestamped snapshots of what a deploy replaced (a single `.env.backup` slot would be
  overwritten by a concurrent converge); the newest of them is not necessarily healthy.
- **`rollback.sh` refuses a rollback that crosses a migration boundary**, before it edits `.env`.
  It reads the target image's highest migration version and the phase markers out of that image
  and compares them with `schema_migrations.version`. It never runs a down-migration; a crossing is
  a human procedure documented in `DEPLOYMENT.md` § "Rolling back across a migration boundary".
- **A service that needs to shut down gracefully needs `stop_grace_period`.** Compose's default is
  10s and then SIGKILL, so a drain, an in-flight cron wait or a connection-pool close that takes
  longer never completes — and the shutdown code that implements it is dead code that reads as
  live. If you add a drain, add the grace period in the same change.

### Shell scripts

Every `.sh` file must pass `shellcheck` (`--severity=warning`) and `/bin/bash -n`. Operators run
these from macOS, whose `/bin/bash` is **3.2**, so:

- **`case` is forbidden inside a block that travels through an unquoted heredoc.** bash 3.2 counts
  parentheses to find the end of the enclosing `$( )`, so a `case` pattern's `)` truncates the
  script. Use `if`/`elif`, and keep every paren in such a block balanced.
- The blocks `deploy-remote.sh` and `rollback.sh` share are compared **byte for byte** by
  `deploy-transition-test.sh`, which also renders them through the heredoc and asserts nothing
  expanded. Edit **both** copies in the same pass, or the test fails and costs another round trip.
- `scripts/shellcheck-all.sh` is the one definition of the file list and severity; `infra/Makefile`
  and the merge gate both call it, with scopes `infra` and `outside-infra`. Don't hand-write a glob
  in either — the list comes from `git ls-files '*.sh'` so a new script is covered the day it lands.

## Observability as code (`grafana/`)

### Instrument every feature and behaviour change

A change that alters what the product does is not finished until it is observable. Before opening
the PR, decide for each of these and say in the body what you did — including "nothing, because…":

- **Product metrics.** A new user-facing flow, or a new outcome of an existing one, needs a
  counter. Extend the existing series rather than minting a parallel one: a new terminal state on
  an existing flow is usually a new label value on the counter that already tracks it. Adding a
  label *value* is cheap; adding a label *key* multiplies cardinality on every series, so justify
  it. Never put a user id, email or capability in a label — those are unbounded and are PII.
- **Logs.** One line at the decision point, at the level that matches the consequence, obeying the
  PII and capability rules above. A silent branch is one you cannot debug from production.
- **Traces.** If the change adds a network hop, a database call or a background job, it should
  appear on the span it belongs to. Span attributes are subject to the same redaction as logs.
- **Alerts.** If the change introduces a failure mode nobody would notice, it needs a rule — and
  the reverse: **if it adds a new non-success outcome to a flow an alert already watches, check
  the alert first.** A rule firing on `status != "success"` will page on a new *expected* outcome.
  That has happened here.
- **Dashboards.** If you added or renamed a series, or changed a label value, update the panel
  that reads it in the same PR — a dashboard querying a series that no longer exists shows an
  empty graph, not an error.

Analytics events are a product surface too: adding or renaming one, or changing a `distinct_id`,
changes what the PostHog funnels mean. Say so in the PR body, because those are not in this repo.

### Shipping the two halves

They ship completely differently, and mixing them up is how alerting goes silent:

- **`grafana/dashboards/` IS Git-Synced**, hourly, from `main`, into folder uid `repository-7b3d712`.
  A merge changes live Grafana with no operator action.
- **`grafana/alerting/` is NOT synced by anything.** The YAML is the versioned source of record;
  the rules are *desired state* until an operator PUTs the group
  (`PUT /api/v1/provisioning/folder/openmentor-alerts/rule-groups/openmentor`, header
  `X-Disable-Provenance: true`). **If a PR changes an alert rule, say so in the PR body** — nothing
  else will tell the operator that a re-apply is owed.
- The rules live in folder uid **`openmentor-alerts`**, deliberately not in the Git Sync dashboards
  folder: Grafana refuses to store alert rules in a Git-Sync-managed folder. **Deleting a Grafana
  folder silently deletes every alert rule in it.** That has happened here — the first apply used a
  folder that looked like a duplicate of the dashboards folder, someone tidied it, and all 14 rules
  went with it, leaving a period with zero alerting.
- **Validate new PromQL against the live tenant before shipping it.** Grafana Cloud Adaptive
  Metrics aggregates some series on this tenant and strips labels from them — `container_spec_memory_limit_bytes`
  has no `name` label here, so the obvious `/ on (name) group_left ()` join errors out, which is
  why the memory limits are written out as literals and pinned against `docker-compose.yml` by
  `alert-fireability-test.sh`. And a rule whose query returns nothing while carrying
  `noDataState: OK` sits permanently green: it is not alerting, it is decoration.
- SLO burn alerts in `grafana/slo/slos.yaml` materialise as their own rules in the separate
  `grafana-slo` folder, so availability and latency page from two independent places. Check both
  before concluding nothing alerts on something; silence both when you silence one.

## CI

- `Checks / required-checks` (`.github/workflows/checks.yml`) runs on every PR and is the **ONLY**
  check that should be required. `CI / Web` and `CI / API` are path-filtered, so requiring their
  job names deadlocks any PR that doesn't touch that subtree.
- Keep the gate **one job**. It is ~20 steps behind 7 path filters; a second required job name
  reintroduces that deadlock.
- The gate owns the fast checks exclusively. The deep workflows keep only what it can't cheaply
  do: production build, race + coverage floor, gosec SARIF, Docker smoke test, DB-backed tests.
  Don't add a fast check to both — that duplication once made a web PR lint twice and `go vet`
  run three times.
- Some path filters deliberately cross directories: `grafana/alerting/**` and
  `grafana/dashboards/**` run the infra gate (`alert-consistency-test.sh` pins alert thresholds
  against `docker-compose.yml`), `api/migrations/**` runs it too (`rollback-migration-guard-test.sh`
  reads `../api/migrations`), and a `docs/audit/2026-08-remediation-plan.md` edit runs the runbooks
  gate. Preserve those.
- Known gap: `deploy-transition-test.sh` asserts properties of `.github/workflows/deploy.yml`, but
  the `infra` filter does not list that path — a deploy.yml-only PR skips that assertion until the
  next push to `main`, where every step runs unconditionally.
- `uses:` are pinned to full 40-char SHAs with a version comment, and Dependabot's
  `github-actions` ecosystem refreshes them. Pinning without that ecosystem freezes the actions
  rather than protecting them.

## Testing

- Every fix needs a test verified to **fail without it**. Say how you checked; reverting the
  production hunk and re-running is the cheap way.
- Prefer fakes at the repository boundary over reimplementing production logic in a mock. A mock
  that silently accepted NULL into a `*string` is exactly why the whole nullable-column defect
  class survived review.
- **SQL guarantees need a real Postgres.** Transaction, single-affected-row and single-use
  behaviour cannot be proved by a mock, and `test/dbtest` skips silently without
  `OPENMENTOR_TEST_DATABASE_URL` — so `go test ./...` goes green having proved none of them.
- Don't let a test pin a bug. One assertion was literally named `dry run exits 0` and protected
  the defect it described.

## Commits and pull requests

- Short, imperative commit subjects; no conventional-commit prefixes. Describe the change, not
  the ticket.
- A PR body says what changed, the evidence for each claim, and anything deliberately left
  undone with the reason. State plainly when something is unverified rather than implying it
  passed. If it changes an alert rule, say so — see the observability section.
- Run the relevant `make ci` / `make check` before opening a PR. Don't report `make ci` as passing
  from a partial run: `api`'s is `lint test-race`, and the race detector — the slow half people
  cut — is where the goroutine bugs surface.
- Keep commits to semantic changes. Do not create `.md` files summarising a job run.
