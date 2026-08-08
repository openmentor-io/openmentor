# CLAUDE.md

Guidance for Claude Code when working in the openmentor monorepo.

## Map

- `web/` — Next.js 16 frontend, **Pages Router only**. Own `CLAUDE.md` + `AGENTS.md` with conventions; work from `web/` (`make ci`, or `npm run dev/test/lint`, `npx tsc --noEmit`).
- `api/` — Go backend, module `github.com/openmentor-io/openmentor/api`. Three binaries: `cmd/api`, `cmd/worker`, `cmd/migrate`. Verify with `make ci` from `api/` (or `make lint` / `make test`).
- `infra/` — Compose/Traefik deployment + observability. Verify with `make check` from `infra/` (6 suites: compose renders, per-service env allowlist + secret ownership, backup sidecar, alert consistency, deploy transition, database identities). Falls back to `.env.example` when there is no `.env`.
- `grafana/` — dashboards and alert rules as code. `dashboards/` is Git-Synced; `alerting/` is **not** (see Observability).
- `docs/` — `docs/migration/DECISIONS.md` (decision log), `docs/runbooks/` (operator procedures), `docs/audit/` (the 2026-08 audit record; §4 lists claims later verification overturned — read it before acting on that plan). `docs/migration/` is a historical record of the getmentor→openmentor fork; don't "fix" old paths there.
- `brand/` — brand asset pack. Never redraw the logo; reference files verbatim (see `brand/README.md` rules). Served copies live in `web/public/brand`.

## Rules

- Cross-cutting changes (API contract, env vars, compose services) land as ONE commit/PR touching all affected directories — that's the point of the monorepo.
- Product/architecture decisions get a row in `docs/migration/DECISIONS.md`. **When several branches run in parallel, reserve an id block per branch** — four branches once all claimed `D39` and reconciling it cost real effort.
- For every new feature, create a separate git branch; never merge to main without explicit permission.
- Never commit real `.env` files or secrets; templates are `*.example` (root `.gitignore` enforces this — don't weaken it).
- **The repository is public.** Don't commit a live capability value (a `client_requests.id`, a token), and don't add exploitation detail for a vulnerability that isn't fixed yet.

### Migrations

- Every migration declares `-- phase: expand` or `-- phase: contract` on line 1 and ships a `.down.sql` documenting any lossiness. A `contract` migration ships a release **after** the code that stops reading the old shape.
- `make migration-check` (`api/scripts/migration-version-check.sh`) fails on duplicate versions, a missing up/down pair, gaps, and a version not greater than main's highest. `make migration-test` applies up → down → up. Both run in the CI gate.
- **Two branches adding the same version number do not conflict in git** — differently-named files merge cleanly and leave golang-migrate with duplicate versions. `Up()` only walks forward, so an out-of-order merge means the skipped migration is *never applied in production* while every dev database gets it, with no error. That is what `migration-check` exists to catch.
- Never run a lossy down-migration automatically. `infra/rollback.sh` refuses a rollback across a migration boundary and points at `infra/DEPLOYMENT.md`.

### Database writes

- A conditional write must require **exactly one affected row**: return `applied bool` from `RowsAffected` and gate side effects (emails, triggers, metrics) on it. The pattern lives in `api/internal/worker/repository.go`.
- Compare-and-set on **the value you read**, not just a status. Writing `status='draft'` under `WHERE status='draft'` is not exclusive — Postgres re-evaluates the predicate after the row lock and it still matches, so N concurrent callers all "win".
- `mentorSelect` in `api/internal/repository/mentor_repository.go` carries the rule: **every nullable column is COALESCEd there, or lands in a pointer field of the model.** pgx rejects NULL into a non-pointer and fails the whole row — one column once broke login, the public profile page and the entire catalog. `mentor_nullable_columns_db_test.go` enumerates nullable columns from `information_schema`, so a newly added one is caught without anyone remembering this file.
- A row and its tags belong in one transaction. A tags failure must not report success.

### Logging, PII and capabilities

- **Never log an email address, personal name, contact detail or request text.** Mask rather than hash low-entropy PII — a hashed address in Loki is a "confirm this person is a user" oracle.
- **Never log a capability**: `client_requests.id`, login/confirmation tokens, review tokens. Use `redact.ID` (SHA-256 → 12 hex) so the line still ties back to one record.
- `logger.RedactedError` is the house style — `api/internal/worker` has **zero** `zap.Error`. An error string carries whatever the caller put in it, including a URL with an id, or a third party quoting your input back in its rejection.
- `api/pkg/redact` is authoritative: `Text`, `ID`, `Path`, `Query`, `URL`, `IsUUID`, `SensitiveKey`. `web/src/lib/redact.ts` mirrors it. Extend the package rather than adding another masker at a call site.
- Redaction also lives in `api/pkg/tracing/redact.go`, which wraps the OTLP **exporter**. A SpanProcessor cannot do it: `otelhttp` sets its attributes after `tracer.Start`, so `OnStart` runs too early and `OnEnd` gets a read-only span.

### Reuse these, don't reinvent

- `api/pkg/safego` — recovered detached goroutines. **No bare `go func()` on a request path.** `recover()` is per-goroutine, so Gin's recovery middleware cannot catch one; a nil-pointer panic in a detached upload goroutine once killed the whole API process *after* the request had returned 200.
- `api/test/dbtest` — shared Postgres bootstrap. It takes an advisory lock because the migrations are not all idempotent and `go test ./...` runs packages as concurrent processes. DB tests skip unless `OPENMENTOR_TEST_DATABASE_URL` is set; the deep `CI / API → Run Tests` job supplies a postgres service.
- `api/internal/middleware/admission.go` caps in-flight uploads; `api/pkg/imageclass` bounds decode via `MaxPixels`/`MaxDecodeBytes` plus a decode semaphore. Those constants are a memory budget against the container's `mem_limit` and are pinned by tests — changing one means re-deciding the budget, not just editing a number.
- `api/config` validates **per binary** (`ValidateForAPI`, `ValidateForWorker`, `ValidateForMigrate`) and fails startup fast. Don't put a requirement in the shared path that only one binary needs: doing so once forced S3 credentials into `migrate` and `worker`, undercutting the per-service secret allowlist.

### CI

- `Checks / required-checks` runs on every PR and is the ONLY check that should be required — `CI / Web` and `CI / API` are path-filtered, so requiring their job names deadlocks any PR that doesn't touch that subtree.
- Keep it **one job**. It is now ~18 steps behind 5 path filters; a second required job name reintroduces that deadlock.
- The gate owns the fast checks exclusively; the deep workflows keep only what it can't cheaply do (production build, race + coverage floor, gosec SARIF, Docker smoke test, DB-backed tests). Don't add a fast check to both — that duplication is what made a web PR lint twice and `go vet` run three times.
- Some path filters deliberately cross directories: a `grafana/` change runs the infra gate (`alert-consistency-test.sh` pins alert thresholds against `docker-compose.yml`), a `deploy.yml` change runs it too (`deploy-transition-test.sh` asserts that workflow's own properties), and a `docs/audit/2026-08-remediation-plan.md` edit runs the runbooks gate. Preserve those.
- Lint/test commands live in `api/Makefile`, `web/Makefile` and `infra/Makefile`, and the workflows call those targets — so `make lint` locally and CI run the identical check. Don't inline a command in a workflow that differs from its make target; that is how CI went green while `make lint` reported 44 issues. `golangci-lint` is pinned via `GOLANGCI_VERSION` in `api/Makefile`, which the workflow reads; `scripts/shellcheck-all.sh` owns the shell file list and severity for both callers.
- The Docker smoke test uses **one env file per binary**. A shared file only ever proves the union of their config contracts boots all three.
- `uses:` are pinned to full 40-char SHAs with a version comment, and Dependabot's `github-actions` ecosystem refreshes them. Pinning without that ecosystem freezes the actions instead of protecting them.

### Observability as code

- `grafana/dashboards/` **is** Git-Synced (repository `repository-7b3d712`, branch `main`, path `grafana/dashboards`, hourly). A merged dashboard reaches Grafana with no operator action.
- `grafana/alerting/` is **not** synced. Alert rules and notification policies are desired state until an operator PUTs the rule group. Rules live in folder uid `openmentor-alerts` — a Git-Sync-managed folder *refuses* alert rules. If a PR changes a rule, say so in the PR body: nothing applies it.
- **Deleting a Grafana folder silently deletes every alert rule in it.** That has happened once and produced a second period with zero alerting.
- Validate a new PromQL expression against the live tenant before shipping a rule. Adaptive Metrics aggregates some series and strips labels (`container_spec_memory_limit_bytes` loses `name`), so an expression that reads correctly can return nothing — and a rule returning nothing with `noDataState: OK` sits permanently green, which is a failure this repo has had twice.
- Metric route labels in `web/` are compile-time literals and are live dashboard dimensions. Changing one changes the panels' series.

### Deploy and operations

- `./deploy.sh` default target does **not** sync `infra/`; pass the `infra` target for compose or env changes.
- Migrations apply automatically: the `migrate` service runs on every converge and backend/worker wait behind `service_completed_successfully`.
- The rollback target is `.env.lastgood`, written only **after** health checks pass. Timestamped `.env.backup.<epoch>` snapshots are history, not the target.
- A service needs `stop_grace_period` for a graceful shutdown path to actually run — without it Docker SIGKILLs partway through and any drain is dead code.
- Shell scripts must pass `shellcheck` and `/bin/bash -n` under **bash 3.2** (macOS, where operators run them). `case` patterns inside an unquoted heredoc break bash 3.2's paren counting; `if/elif` is safe.
- Env contracts: `infra/.env.example` and `.env.production.example` must stay consistent with what `api/config/config.go` and `web/` actually read. There is no shared `env_file` any more — every service has an explicit `environment:` allowlist enforced by `infra/check-service-env.sh` against `infra/env-allowlist.txt`. **A new env var must be added to the allowlist**, and a bare `- KEY` entry renders away when unresolved, so it passes the check while reaching no container.

### Testing

- Every fix needs a test verified to **fail without it**. Say how you checked; reverting the production hunk and re-running is the cheap way.
- Prefer fakes at the repository boundary over reimplementing production logic in a mock. A mock that silently accepted NULL into `*string` is exactly why a whole defect class survived review.
- Transaction, single-affected-row and single-use guarantees are SQL properties — prove them against a real Postgres with concurrent callers, not a fake.
- Don't let a test pin a bug. One assertion was literally named `dry run exits 0` and protected the defect it described.
