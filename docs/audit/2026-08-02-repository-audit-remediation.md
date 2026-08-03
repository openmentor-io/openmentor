# Repository Audit Remediation Implementation Plan

> **⚠️ Superseded — evidence only.** Use
> [`2026-08-remediation-plan.md`](./2026-08-remediation-plan.md) as the actionable plan. It reconciles
> this plan with a second audit and with empirical verification, and deliberately scales back two tasks
> here: adopting River (Task 7/8) is **not** immediate work, and the review-token expand/contract
> sequence (Task 2) is sized by an actual count first. See §4 and §12 of the plan for the reasoning.
> Retained as a record of the original recommendations.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the verified trust-boundary, authentication, state-integrity, recovery, and maintainability defects documented in the 2026-08-02 repository audit without adding unnecessary platform complexity.

**Architecture:** Retain the Next.js BFF, Go modular monolith, PostgreSQL database, and current single-VM deployment. Tighten service/environment contracts, make relational transitions atomic, add a PostgreSQL-backed River job/outbox path, and make deployment/recovery fail closed. Prefer small typed helpers over new frameworks.

**Tech Stack:** Go 1.26.5, PostgreSQL 16, River for durable jobs, Next.js 16 / React 19 / TypeScript, Jest, Go testing, Docker Compose, GitHub Actions, Grafana Alloy/Cloud, PostHog.

---

## How to execute this plan

- Read the companion audit first: [`2026-08-02-full-repository-audit.md`](./2026-08-02-full-repository-audit.md).
- Use one pull request per task unless a task explicitly names a shared PR boundary. Do not combine unrelated cleanup with a security fix.
- Begin every behavior change with a failing regression test. Keep commits reviewable and run the task-specific command after each logical edit.
- Historical files under `docs/migration/` are immutable. Correct live runbooks elsewhere; do not rewrite history.
- Migration numbers below assume `000009` remains the latest. At implementation time, allocate the next unused version rather than colliding with concurrent work.
- Never print secret values in tests, Compose validation, CI logs, or support bundles.
- Any task that changes production credentials, database roles, backup behavior, or recovery instructions requires a staging rehearsal and named operator sign-off before production.

## Dependency map and release order

| Wave | Tasks | Release rule |
|---|---|---|
| Incident containment | 1, 2, and 3 | Deploy immediately; Task 1A may run alongside; rotate/purge only after code mitigation is live |
| Urgent security and state integrity | 4–10 plus 9A | Complete before normal feature work resumes |
| Production boundary/recovery | 11–17 plus 12A | Rehearse in a production-shaped staging VM |
| Data and web correctness | 18–25 plus 24P | Independent bounded PRs after P0/P1 controls |
| Maintainability and cleanup | 26–29 | Only after regression coverage exists |

Tasks 1, 1A, 3, and 4 can proceed in parallel. Task 2 depends on Task 1's sanitizer contract. Task 9 is an immediate callback-integrity fix and does not wait for River. Task 7 establishes the durable job foundation; Task 8 and the later transformation half of Task 10 use it. Task 13 depends on Task 12's environment/database contract. Task 28 should land after the affected BFF behavior has tests from earlier tasks.

## Wave 0 — Incident containment

### Task 1: Sanitize sensitive URLs before tracing and logging

**Findings:** SEC-02
**PR boundary:** `security/redact-sensitive-observability`
**Files:**

- Create: `web/src/lib/sensitive-data.ts`
- Create: `web/src/lib/__tests__/sensitive-data.test.ts`
- Modify: `web/src/lib/tracing-server.ts`
- Modify: `web/src/lib/logger.ts`
- Modify: `web/src/lib/with-observability.ts`
- Modify: `web/src/lib/posthog.ts`
- Modify: `web/src/lib/faro.ts`
- Test: `web/src/lib/__tests__/with-observability.test.ts`
- Create: `api/internal/middleware/sensitive_data.go`
- Modify: `api/internal/middleware/observability.go`
- Modify: `api/cmd/api/main.go`
- Modify: `api/internal/services/review_service.go`
- Modify: `api/internal/services/contact_service.go`
- Test: Go tracing/logger/tracker tests with in-memory collectors

- [ ] Define one pure sanitizer with signatures `sanitizeUrl(value: string): string` and `sanitizeAttributes<T extends Record<string, unknown>>(attributes: T): T`. Treat normalized keys `token`, `requestid`, `confirmationtoken`, `authorization`, `cookie`, and future capability-token keys as sensitive. Replace values with `[REDACTED]`; do not merely remove query separators and accidentally alter route identity.
- [ ] Add failing table tests for absolute/relative URLs, query strings, capability-bearing path segments, percent encoding, repeated keys, camel/snake case, arrays, nested attributes, malformed URLs, and benign values.
- [ ] Run `cd web && npm test -- --runInBand src/lib/__tests__/sensitive-data.test.ts`; expect the new tests to fail before implementation.
- [ ] Implement the pure sanitizer without logging the rejected/raw value.
- [ ] Configure HTTP `startIncomingSpanHook` and Undici `startSpanHook` in `tracing-server.ts` to replace `url.query`, `url.full`, `url.path`, and legacy `http.url`. Add an exporter/span-processor defense so future instrumentation cannot bypass the hooks.
- [ ] Remove the unused/dangerous request-header capture allowlist from HTTP instrumentation.
- [ ] Sanitize request URLs before the logger or error context receives them. Ensure metric route labels use their own static contract and are not based on the sanitized raw URL.
- [ ] Add an in-memory OpenTelemetry exporter test that issues incoming `/mentor/auth/callback?token=audit-secret` and outgoing review capability requests, then recursively asserts the sentinel is absent from every attribute.
- [ ] Add the equivalent Go sentinel test around `otelgin`, request/error logging, and the analytics tracker. Sanitize actual paths and error route parameters, and remove capability identifiers from service analytics properties/distinct IDs.
- [ ] Immediately suppress the legacy capability-bearing review routes with `otelgin` filtering (or equivalent pre-span exclusion) until Task 2 removes the secret from the path. For retained routes, configure/wrap instrumentation so it exports only safe route-template attributes. Do not rely on post-export cleanup as the primary control.
- [ ] Run `cd web && npm test -- --runInBand && npm run lint && npx tsc --noEmit`; expect all checks to pass.
- [ ] Run `cd api && go test -race ./internal/middleware ./internal/services`; expect all sentinel tests to pass.

### Task 1A: Bound web metric cardinality with explicit route templates

**Findings:** OBS-01
**PR boundary:** `security/static-bff-metric-labels`
**Files:**

- Modify: `web/src/lib/with-observability.ts`
- Modify: all wrappers under `web/src/pages/api/`
- Modify/Test: `web/src/lib/__tests__/with-observability.test.ts`

- [ ] Change the wrapper contract to require a compile-time label, for example `withObservability('/api/mentor/requests/:id', handler)`. Do not infer label identity from `req.url`.
- [ ] Add a failing test that sends thousands of unique non-UUID and malformed path values before authentication and inspects the Prometheus registry; the series count must remain fixed.
- [ ] Migrate API routes in batches of at most five, using the Next file route template as the label. Reject an empty or user-derived label in development/test.
- [ ] Keep sanitized raw URL context only in logs; metrics receive method, static route, and bounded status class/code labels.
- [ ] Run `cd web && npm test -- --runInBand src/lib/__tests__/with-observability.test.ts && npm run lint && npx tsc --noEmit`.

### Task 2: Remove capabilities from analytics and replace review primary-key authorization

**Findings:** SEC-02
**Depends on:** Task 1
**PR boundaries:** 2A containment; 2B expand/dual-read; 2C invitation cutover; 2D contract cleanup
**Files:**

- Add migration: `api/migrations/000010_review_capability_tokens.up.sql`
- Add migration: `api/migrations/000010_review_capability_tokens.down.sql`
- Modify: `api/internal/models/mentor_client_request.go`
- Modify: `api/internal/repository/review_repository.go`
- Modify: `api/internal/services/review_service.go`
- Modify: `api/internal/handlers/review_handler.go`
- Modify: review URLs in `api/internal/worker/`
- Modify: `web/src/pages/mentor/[slug]/contact.tsx`
- Modify: `web/src/pages/reviews/new.tsx`
- Modify: `web/src/pages/api/reviews/check.ts`
- Modify: `web/src/pages/api/reviews/submit.ts`
- Modify/Test: `web/src/lib/analytics.ts` and its tests
- Modify/Test: `api/pkg/email/templates/assets/session-complete.json`

- [ ] Add failing analytics tests showing exact/nested `request_id`, `requestId`, `review_token`, and normalized variants are dropped, not merely URL-scrubbed.
- [ ] **2A — containment:** deploy Task 1 and remove current request IDs from every web/Go analytics, log, trace, and error context before changing authorization.
- [ ] Define the token contract: at least 32 random bytes, stored only as a hash, single-use, and an explicit expiry duration approved by product/security. Record how outstanding invitations are identified, reissued, or deliberately invalidated.
- [ ] **2B — expand:** add nullable hash/expiry/consumed fields and generate tokens for new completions. Carry the plaintext between BFF and Go only in a POST body or secret header excluded from instrumentation—never a URL path/query. Temporarily retain a sanitized legacy lookup for existing links.
- [ ] Update `session-complete.json` and all link constructors to use the new token only after the expand deployment is healthy.
- [ ] Add API concurrency tests: two submissions using one token yield one success and one typed 409; expired/consumed tokens reveal no request details. Add Go/web sentinel tests proving transport does not leak it.
- [ ] **2C — cutover:** reissue outstanding invitations or explicitly invalidate them before disabling request-ID authorization. Define the dual-read deadline, operator query/count, user communication, and rollback to the expand release without restoring telemetry leakage.
- [ ] **2D — contract:** remove legacy request-ID authorization and later remove compatibility schema/code after the deadline. Keep the request UUID internal.
- [ ] Run `cd api && go test -race ./internal/services ./internal/repository ./internal/handlers` and `cd web && npm test -- --runInBand`; expect both to pass at every phase.

### Task 3: Introduce per-service environment allowlists

**Findings:** SEC-01
**PR boundary:** `security/per-service-runtime-env`
**Files:**

- Modify: `infra/docker-compose.yml`
- Modify: `infra/deploy-remote.sh`
- Modify: `infra/deploy.sh`
- Modify: `infra/rollback.sh`
- Modify: `infra/ENVIRONMENT_VARIABLES.md`
- Modify: `infra/.env.production.example`
- Create: `infra/scripts/validate-service-env.sh`
- Modify: `.github/workflows/checks.yml`

- [ ] Inventory configuration reads per binary with `rg 'Getenv|LookupEnv|process\.env|env\(' api web infra` and record the intended service-to-key matrix in `infra/ENVIRONMENT_VARIABLES.md`.
- [ ] Add a failing validation script that renders Compose to JSON and compares key names—not values—against strict committed allowlists for frontend, backend, worker, migrate, backup, Alloy, Traefik, PostgreSQL, and cAdvisor.
- [ ] Remove shared `.env.runtime` from every service. Declare only needed values in each service's `environment:` block; prefer service-specific root-owned env files only if the explicit list becomes unmaintainable.
- [ ] Ensure build/deploy values (`VM_*`, source-map upload credentials, Cloudflare control credentials) never enter runtime containers.
- [ ] Separate worker-callback authority from unrelated services. The frontend must not receive `WORKER_AUTH_TOKEN`, JWT signing keys, database credentials, SES credentials, or backup credentials.
- [ ] Update deploy/rollback generation without printing values and remove obsolete `.env.runtime` handling after a one-release compatibility window if necessary.
- [ ] Run `docker compose --env-file infra/.env.example -f infra/docker-compose.yml config -q` and the new allowlist script; expect no forbidden key or blank instance warning.
- [ ] Rehearse staging startup for every service and document the exact external credential rotation order. Rotate database, backup, S3, SES, Cloudflare, JWT, worker, Grafana, and upload credentials only after the least-privilege deployment is healthy.

## Wave 1 — Urgent security and state integrity

### Task 4: Centralize safe transactional-email rendering

**Findings:** SEC-03, PRIV-01
**PR boundary:** `security/escape-email-template-data`
**Files:**

- Create: `api/pkg/email/render.go`
- Create: `api/pkg/email/render_test.go`
- Modify: `api/pkg/email/sender.go`
- Modify: callers under `api/internal/worker/job_*.go`
- Modify/Review: all files under `api/pkg/email/templates/assets/`

- [ ] Inventory every `(template, placeholder, context)` occurrence across subject, HTML text node, HTML attribute, URL, and plain-text body. A property-name-only classification is insufficient because one placeholder can occur in several contexts.
- [ ] Move rendering into the application: parse HTML with Go `html/template`, subject/text with `text/template`, use `missingkey=error`, and send fully rendered SES `Simple` content. Convert embedded placeholder syntax deliberately and add a test that every template parses and renders. Do not build a second ad hoc replacement engine.
- [ ] Keep request-derived values as ordinary strings so `html/template` applies contextual escaping. Introduce a narrowly scoped `template.HTML` wrapper only for reviewed server-generated fragments that cannot contain request data. Validate configured links as HTTPS/allowed-origin values before rendering.
- [ ] Add failing tests with closing tags, branded phishing links, images, entity/encoding variants, quotes in attributes, and `javascript:`/`data:` URLs for contact, review, registration, and moderation mail.
- [ ] Assert rendered subject, HTML, and plain text separately. HTML must contain escaped text/no injected node or attribute; plain text must remain human-readable (`&`, not `&amp;`); subjects must not contain control/newline injection.
- [ ] Remove caller-specific ad hoc escaping only after parity tests prove the context-aware renderer owns it.
- [ ] Render every embedded template with a fixture, parse the HTML result, and assert intentional server-generated fragments still render.
- [ ] Run `cd api && go test -race ./pkg/email ./internal/worker/...`; expect all tests to pass.

### Task 5: Consume login and confirmation tokens atomically

**Findings:** AUTH-01, DB-02
**Files:**

- Add migration: next unused `*_atomic_auth_tokens.{up,down}.sql`
- Modify: `api/internal/repository/mentor_repository.go`
- Modify: `api/internal/repository/moderator_repository.go`
- Modify: `api/internal/services/mentor_auth_service.go`
- Modify: `api/internal/services/admin_auth_service.go`
- Modify: `api/internal/services/mentor_confirmation_service.go`
- Add repository/service integration tests

- [ ] Write concurrent service tests that synchronize two verification calls at the old read/clear boundary; prove both currently succeed.
- [ ] Add a partial unique index on non-null mentor login token hashes. Store new confirmation tokens as SHA-256 (or the same reviewed one-way token-hash format as login tokens) with a partial index; compare through indexed hash lookup without loading plaintext tokens.
- [ ] Implement repository methods `ConsumeLoginToken(ctx, hash, now) (Account, error)` and `ConsumeConfirmationToken(...)` using one conditional `UPDATE ... RETURNING` statement.
- [ ] Delete separate read/clear success paths. Any query/update error must fail closed and mint no JWT/event.
- [ ] Define exact typed outcomes: consumed, expired, not found, wrong state, and database failure. Confirm may consume only a matching unexpired token on a draft mentor; resend atomically rotates a draft token and preserves the currently supported expired-token case.
- [ ] Roll out confirmation hashing with an explicit compatibility choice: either planned invalidation/reissue of all outstanding confirmation links or a short sanitized dual-read migration. Record the deadline and remove plaintext support in a contract release.
- [ ] Insert the confirmation event/River job in the same transaction as the successful state transition once Task 7 is available; until then, do not weaken atomic token consumption to accommodate the old trigger.
- [ ] Run repository integration tests against PostgreSQL plus `cd api && go test -race ./internal/services/...`; expect exactly one winner in every race.

### Task 6: Tighten JWT contracts and privileged session revocation

**Findings:** AUTH-02
**Files:**

- Modify: `api/pkg/jwt/jwt.go`
- Add: `api/pkg/jwt/jwt_test.go`
- Add migration: next unused `*_session_versions.{up,down}.sql`
- Modify: mentor/moderator models and repositories
- Modify: `api/config/config.go`
- Modify: `api/internal/middleware/admin_session.go`
- Modify: `api/internal/middleware/mentor_session.go`
- Modify: auth handlers/services and `.env.example`

- [ ] Add failing tests for HS512, wrong issuer/audience, missing expiry/issued-at/subject/token type, malformed subject UUID, and cross-realm token use.
- [ ] Validate using exact HS256 plus required issuer, audience, timestamps, subject, and token type. Configure distinct admin and mentor audiences/signing secrets.
- [ ] Add an indexed/account-local `session_version` column, repository lookup, and JWT claim. Re-read admin role/version on every privileged request; verify mentor allowed status/version for mutation routes.
- [ ] Increment/revoke session version on logout-all, role removal, decline, deactivation, and security rotation. Keep ordinary browser logout cookie-only unless product requires global logout.
- [ ] Coordinate distinct-key rotation with Task 3 so production has one intentional logout, not two. Choose either a planned global logout (simplest) or a short dual-validation window with an explicit removal deadline. Adding required audience to old tokens without this rollout is forbidden.
- [ ] Return 401/403 for invalid/missing claims, never a recovered 500.
- [ ] Run `cd api && go test -race ./pkg/jwt ./internal/middleware ./internal/handlers`; expect all checks to pass.

### Task 7: Establish River as the transactional job store

**Findings:** JOB-02
**PR boundary:** infrastructure/foundation only; migrate one harmless event as proof
**Files:**

- Modify: `api/go.mod`, `api/go.sum`
- Add River schema through the pinned version's official migration mechanism
- Create: `api/internal/jobs/` package for typed job arguments and registration
- Modify: `api/cmd/api/main.go`
- Modify: `api/cmd/worker/main.go`
- Modify: `api/config/config.go`
- Add: integration tests under `api/test/internal/jobs/`

- [ ] Select and pin a maintained River version compatible with pgx v5. Verify and use that version's official versioned migration workflow. Record the decision and schema ownership in a new current ADR under `docs/architecture/decisions/`; do not modify immutable `docs/migration/DECISIONS.md`.
- [ ] Use River's transaction-aware job insertion as the durable outbox. Do not create a parallel application outbox table/poller unless a later ADR demonstrates a concrete non-River integration requirement.
- [ ] Add a typed job with a unique idempotency key and a no-op/test consumer; write an integration test proving commit makes it visible and rollback does not.
- [ ] Configure bounded attempts, exponential backoff/jitter, queue limits, timeouts, and terminal error visibility using River's native facilities.
- [ ] Start River only in the worker binary. API code enqueues through a narrow interface; migration binary does not initialize unrelated integrations.
- [ ] Implement graceful stop/drain and health/readiness that distinguishes database connectivity, queue polling, and terminal failures.
- [ ] Run `cd api && go test -race ./internal/jobs/... ./cmd/worker/...`; expect retry/idempotency/shutdown tests to pass.

### Task 8: Move authentication and notification delivery to durable jobs

**Findings:** JOB-02
**Depends on:** Tasks 4, 5, 7
**PR series:** 8A login; 8B registration/confirmation; 8C contact/request; 8D status/review; 8E image/profile
**Files:**

- Modify: `api/internal/services/mentor_auth_service.go`
- Modify: `api/internal/services/admin_auth_service.go`
- Modify: registration/contact/profile/request/review services
- Replace: `api/pkg/trigger/trigger.go`
- Refactor: relevant `api/internal/worker/job_*.go`
- Add integration tests per job

- [ ] Inventory every `trigger.CallAsync` and `UploadImageAllSizesAsync` call with its transaction boundary, ordering need, and idempotency key.
- [ ] Migrate one event family per PR: 8A login email, 8B registration/confirmation, 8C contact/request notifications, 8D status/review notifications, then 8E profile/image work. Keep the old trigger path for that family until its durable-job parity tests pass; never dual-send.
- [ ] Persist the job in the same database transaction as the state change when loss would contradict the API success response.
- [ ] Before sending login email, verify its token/version remains current so delayed jobs cannot deliver a stale link.
- [ ] Split multi-recipient delivery into independently idempotent jobs or persistent recipient states so retry does not duplicate completed recipients.
- [ ] Delete detached trigger goroutines only when call-site parity tests and metrics prove each flow is migrated.
- [ ] Add dashboards/alerts for queue age, retries, terminal failures, and throughput without capability/PII labels.
- [ ] Run `cd api && go test -race ./...`; then kill/restart worker during staging flows and verify recovery.

### Task 9: Make worker callbacks replay-safe during migration

**Findings:** JOB-01
**Depends on:** none; land before River and before all Task 8 flows are migrated
**Files:**

- Modify: `api/internal/worker/job_new_mentor_watcher.go`
- Modify: `api/internal/worker/job_mentor_moderation.go`
- Modify: `api/internal/worker/job_new_request_watcher.go`
- Modify: `api/internal/worker/repository.go`
- Modify: worker job tests

- [ ] Add replay tests for every state-mutating callback after advancing the entity to a later state. Assert no state, credential, analytics, or email change.
- [ ] Remove state repair from notification consumers when the API already owns the transition.
- [ ] Where mutation remains necessary, include event ID and expected state/version, claim idempotently, update with compare-and-swap, and require one affected row.
- [ ] Reject free-form worker email recipients/login URLs; derive recipient and trusted URL from current database state/configuration.
- [ ] Split or scope worker authority so a credential intended for one job class cannot invoke all state-mutating jobs.
- [ ] Run `cd api && go test -race ./internal/worker/...`; expect duplicate/out-of-order tests to pass.

### Task 9A: Make scheduled and migration work single-owner and verifiable

**Findings:** JOB-03
**PR boundaries:** 9A-1 worker cron ownership; 9A-2 migration claims; 9A-3 image-copy verification
**Files:**

- Modify/Test: `api/internal/worker/cron.go` and cron jobs
- Modify/Test: `infra/migration/migrate-mentors.js`
- Modify/Test: `infra/migration/yandex-to-s3-migration.js`
- Add migration: next unused migration-intent lease/attempt fields

- [ ] Add local `cron.SkipIfStillRunning` wrappers and a PostgreSQL advisory lock/lease shared by scheduled and manual invocation. Record idempotent delivery markers for reminder emails.
- [ ] Run two worker schedulers plus a manual invocation in a test; exactly one logical execution/send may occur.
- [ ] Add `processing`, lease owner/expiry, attempt count, and last error to migration intents. Atomically claim through `UPDATE ... RETURNING` or `FOR UPDATE SKIP LOCKED`; recover expired leases and prevent a stale owner from completing a reclaimed item.
- [ ] Run two migration consumers concurrently and assert each intent's external effect occurs once.
- [ ] Store/compare SHA-256 for migrated image content and verify after copy. Existing same-size/different-content and preexisting partial objects must not be accepted as success.
- [ ] Keep each of the three behavior groups in its own PR and rehearse migration changes on copied/non-production data.

### Task 10: Validate images before mutation and make storage lifecycle explicit

**Findings:** SEC-04, CFG-01, DATA-01
**PR boundaries:** 10A immediate bounds/startup contract (no dependency); 10B durable transformations/compensation (depends on Task 7)
**Files:**

- Modify: `api/pkg/imageclass/imageclass.go`
- Modify: `api/pkg/s3storage/storage.go`
- Modify: `api/config/config.go`
- Modify: `api/cmd/api/main.go`
- Modify: `api/internal/services/registration_service.go`
- Modify: `api/internal/services/profile_service.go`
- Add fuzz/unit/integration tests

- [ ] **10A:** add compressed fixtures/fuzz seeds with excessive width, height, pixels, aspect ratio, invalid base64, MIME spoofing, and malformed headers. Assert fast bounded rejection before database calls.
- [ ] **10A:** use `image.DecodeConfig`, set explicit limits, then decode once. Pass one validated image representation to classification and storage preparation.
- [ ] **10A:** choose and document one contract: storage required in production (recommended) or feature explicitly disabled. Validate partial credentials, endpoint, bucket, and region at API startup.
- [ ] **10A:** reject invalid/unavailable image operations before creating or updating mentor rows. Never start a nil-client goroutine. Deploy this PR without waiting for River.
- [ ] **10B:** store one canonical object unless real size variants are generated. If variants remain, generate them with a maintained imaging library in durable jobs and update metadata only after all required objects exist.
- [ ] **10B:** add cleanup/retry semantics for object-success/database-failure and replacement of old images.
- [ ] Run `cd api && go test -race ./pkg/imageclass ./pkg/s3storage ./internal/services/...`; inspect peak memory for bomb fixtures.

## Wave 2 — Production boundary and recovery

### Task 11: Serialize and fail-close deployments

**Findings:** OPS-01, OPS-02
**Files:**

- Modify: `.github/workflows/deploy.yml`
- Modify: `infra/deploy.sh`
- Modify: `infra/deploy-remote.sh`
- Modify: `infra/rollback.sh`
- Add shell tests under `infra/test/`

- [ ] Add GitHub `concurrency` keyed to production with `cancel-in-progress: false`.
- [ ] Acquire one remote `flock` for deploy, rollback, migration, and upgrade entry points. Include a deployment ID in logs/metadata and bind the backup env/release record to it.
- [ ] Validate all operator inputs. Pass rollback tags as positional arguments and accept only full SHA/digest or a strict Docker-tag regex; reject whitespace, quotes, substitutions, and newlines.
- [ ] Make public HTTPS health mandatory with bounded retries. Permit skipping only through an explicit, logged bootstrap/break-glass input.
- [ ] Ensure a failed public probe returns failure and leaves an unambiguous rollback/roll-forward status.
- [ ] Run shell syntax/ShellCheck plus tests that start two deploys and confirm the second waits.

### Task 12: Separate database identities and verify least privilege

**Findings:** DB-01, PRIV-01
**Depends on:** Task 3's service contract
**Files:**

- Add migration/bootstrap SQL under `infra/postgres/` or a new idempotent provisioning script
- Modify: `infra/docker-compose.yml`
- Modify: `infra/.env.production.example`, `infra/.env.example`
- Modify: API/worker/migrate database configuration
- Modify: `docs/runbooks/database-observability.md`
- Add PostgreSQL privilege tests

- [ ] Define owner/bootstrap, migrator, runtime, backup, and monitoring roles with separate credentials and explicit grants.
- [ ] Test that runtime can perform every application query but cannot create roles/extensions, change ownership, drop schema, or read unrelated administrative data.
- [ ] Give the migrator schema-change authority without role-management authority. Give backup only the read/lock permissions required by `pg_dump`.
- [ ] Re-evaluate `pg_read_all_data` for Alloy. Disable schema/explain/query-sample collectors or provide restricted views if monitoring cannot work without broad PII access.
- [ ] Rotate the current database superuser-derived application credential after all services use new roles.
- [ ] Run the full API integration suite with runtime credentials and migrations with migrator credentials.

### Task 12A: Remove direct Docker-daemon access and harden host monitors

**Findings:** SEC-05
**PR boundary:** `security/docker-discovery-boundary`
**Files:**

- Modify: `infra/docker-compose.yml`
- Add: narrowly configured Docker socket proxy service/configuration
- Modify: Traefik provider endpoint/configuration
- Modify: cAdvisor mounts/security options or remove cAdvisor if Task 27 cannot make it useful
- Add: Compose/security contract tests

- [ ] Add a failing contract test that discovers services through the proposed endpoint but cannot `POST /containers/create`, start/exec/kill containers, access secrets, or reach unrelated Docker API endpoints.
- [ ] Put a maintained, pinned Docker socket proxy between Traefik and `/var/run/docker.sock`; allow only the read endpoints Traefik provider discovery actually requires. The socket must not be mounted in Traefik.
- [ ] Verify certificate/router/container discovery, refresh, and container restart behavior in staging before production cutover.
- [ ] For cAdvisor, mount only documented required paths, make root/filesystem/capability exposure explicit, add `read_only`, `cap_drop`, `no-new-privileges`, and restart policy where compatible. If useful metrics cannot be collected safely/reliably, remove it and its dead alerts instead of retaining privileged theater.
- [ ] Pin proxy/cAdvisor images by digest and add the negative Docker API test to the infrastructure merge gate.

### Task 13: Make backup success measurable and templates self-consistent

**Findings:** OPS-03, OPS-04
**Depends on:** Tasks 3 and 12
**Files:**

- Modify: `infra/postgres-backup/backup.sh`
- Modify: `infra/postgres-backup/Dockerfile`
- Modify: `infra/docker-compose.yml`
- Modify: `infra/.env.production.example`
- Modify: `docs/runbooks/postgres-backup-restore.md`
- Modify: Grafana alert configuration

- [ ] Add tests for missing DB credentials, missing dedicated backup credentials, failed dump, failed upload, stale success, and successful off-site backup.
- [ ] Persist last attempt, last success, destination class, and error status in a root-owned status file/metric without secret values.
- [ ] Retry transient failure with bounded backoff; do not hide repeated failure. Add a healthcheck based on freshness (for example, unhealthy after 26 hours) rather than process liveness.
- [ ] Make the production template either require explicit nonempty dedicated credentials with a bucket or leave the bucket empty. Remove every stale fallback claim.
- [ ] Alert on no successful off-site backup within the agreed RPO buffer and surface stale backup status during deployment without making an old backup process look healthy.
- [ ] Break DB and S3 credentials separately in staging and verify health/alerts. Perform and record a restore drill before closing the task.

### Task 14: Rewrite and rehearse restore, rollback, and PostgreSQL upgrade procedures

**Findings:** OPS-05, OPS-06
**Depends on:** Tasks 11–13
**Files:**

- Modify: `docs/runbooks/postgres-backup-restore.md`
- Modify: `docs/runbooks/postgres-16-to-18-upgrade.md`
- Modify: `infra/DEPLOYMENT.md`
- Modify: `infra/README.md`
- Modify: `infra/docs/troubleshooting.md`
- Add: a production-shaped rehearsal harness/checklist

- [ ] Provision a scratch VM containing only what real deployment syncs. Do not add a monorepo checkout or ambient AWS credentials.
- [ ] Fetch/list backups through the backup container's scoped environment and shared backup volume, or document a safe explicit credential mapping before writers are stopped.
- [ ] Stage the exact release/migration artifacts required for rollback; remove commands that assume `/opt/openmentor` is a Git checkout.
- [ ] Define expand/contract policy, N/N-1 compatibility, roll-forward preference, and the operator decision required for incompatible/data-changing migrations. Never auto-run lossy downs.
- [ ] Execute restore and PG major upgrade verbatim on the scratch VM; capture timings, checksums, row-count/application smoke checks, and monitoring restoration.
- [ ] Replace dangerous troubleshooting instructions (`chmod 777`, deleting ACME state, copying infra as DB backup, environment dumps) with links to the rehearsed canonical procedures.

### Task 15: Pin supply-chain inputs and SSH identity

**Findings:** SUP-01, SUP-02
**Files:**

- Modify: all `.github/workflows/*.yml`
- Modify: `.github/dependabot.yml`
- Modify: `infra/deploy.sh`, `infra/rollback.sh`, mentor migration tooling
- Modify: provisioning documentation

- [ ] Resolve every third-party action tag to a reviewed 40-character commit SHA and retain the human-readable version in a comment.
- [ ] Add the `github-actions` Dependabot ecosystem and a policy test rejecting non-SHA `uses:` entries.
- [ ] Minimize GitHub token permissions per job; grant `security-events: write` only to the SARIF upload job and verify the AWS OIDC trust policy is repository/ref/environment constrained.
- [ ] Require a pre-provisioned SSH host key/fingerprint in CI and local production operations. Missing or mismatched identity must abort without TOFU fallback.
- [ ] Run actionlint and simulate missing/mismatched host keys; both must fail as designed.

### Task 16: Make release artifacts reproducible

**Findings:** OPS-07, DEP-01
**Files:**

- Modify: `infra/deploy.sh`
- Modify: `infra/DOCKER_TAG_POLICY.md`
- Modify: `api/Dockerfile`, `web/Dockerfile`
- Modify: CI build/deploy workflow

- [ ] Add preflight tests for dirty tracked files, relevant untracked files, detached/unapproved branch, and unpushed commit.
- [ ] Refuse local production deploy in those states unless an explicit audited break-glass flag is supplied.
- [ ] Build/tag with full commit SHA and deploy the registry digest. Enable ECR immutable tags and verify an overwrite is rejected.
- [ ] Pin base images by full patch version and digest while retaining readable version comments.
- [ ] Build the same revision twice in the same controlled environment and compare provenance/digests; document unavoidable nondeterminism.

### Task 17: Update vulnerable dependencies and align toolchains

**Findings:** DEP-01
**PR boundaries:** 17A Go toolchain/gRPC; 17B Next nested dependency remediation
**Files:**

- Modify: `api/go.mod`, `api/go.sum`, `api/Dockerfile`
- Modify: Go versions in `.github/workflows/*.yml`
- Modify: `web/package.json`, `web/package-lock.json`
- Add/Modify: dependency policy checks

- [ ] Pin Go 1.26.5 consistently across module directive/toolchain, Docker, and CI; re-run all API tests/race/lint/vulnerability scans.
- [ ] Upgrade `google.golang.org/grpc` to at least v1.82.1 through the smallest compatible direct/indirect change; inspect `go mod graph` and avoid unrelated churn.
- [ ] In a branch, test npm `overrides` for Next's nested PostCSS >=8.5.18 and sharp >=0.35.0. If Next cannot run correctly, track/take the first patched Next instead of downgrading the framework.
- [ ] Run `cd web && npm ci && npm run lint && npx tsc --noEmit && npm test -- --runInBand && npm run build`; exercise OG/image paths.
- [ ] Run `govulncheck ./...` and `npm audit --omit=dev`; record remaining accepted/qualified findings with owner and expiry.

## Wave 3 — Data and web correctness

### Task 18: Make profile, tag, and registration writes atomic

**Findings:** DATA-01
**Depends on:** Task 8 for post-commit work
**Files:**

- Modify: `api/internal/services/profile_service.go`
- Modify: `api/internal/services/registration_service.go`
- Modify: `api/internal/services/admin_mentors_service.go`
- Modify: `api/internal/repository/mentor_repository.go`
- Add repository transaction helpers and integration tests

- [ ] Add failure-injection tests at field update, tag validation/write, outbox insert, image metadata, and commit. Assert no partial success.
- [ ] Validate and resolve every tag before beginning registration; reject unknown tags rather than silently dropping them.
- [ ] Add repository methods that accept an existing pgx transaction and update mentor fields/tags/outbox atomically.
- [ ] Define explicit object-storage compensation: a durable cleanup/retry job for objects written before database failure.
- [ ] Ensure `updated_at`/photo revision changes only when the new image is available, so cache busting is truthful.
- [ ] Run `cd api && go test -race ./internal/services ./internal/repository` plus PostgreSQL integration tests.

### Task 19: Gate contact and state transitions in SQL

**Findings:** DATA-02
**Files:**

- Modify: `api/internal/repository/client_request_repository.go`
- Modify: `api/internal/services/contact_service.go`
- Modify: mentor/admin request and profile state services
- Modify: worker repository transition queries
- Add concurrency integration tests

- [ ] Add tests proving hidden, inactive, draft, pending, and nonexistent mentors create no request/event and disclose no calendar URL.
- [ ] Implement contact creation as an atomic `INSERT ... SELECT` gated by active/visible mentor state, returning the safe calendar value from the same current row if needed.
- [ ] Change updates to `... WHERE id=$id AND mentor_id=$mentor AND status=$expected RETURNING ...`; map zero rows to typed conflict/not-found results.
- [ ] Apply compare-and-swap/version checks to profile submission, moderation, and deactivation cron transitions.
- [ ] Persist the corresponding durable event in the same transaction.
- [ ] Race conflicting actions and assert one state and one logical event win.

### Task 20: Align upload contracts and remove fake variants

**Findings:** WEB-01
**Depends on:** Task 10
**Files:**

- Modify: image limits in `RegisterMentorForm.tsx`, `ProfileForm.tsx`, and admin mentor page
- Modify: Next API upload route limits
- Modify: API request limits and image model validation
- Modify: image loader/storage-key consumers if canonical path changes

- [ ] Add serialized boundary tests at just below/equal/above every declared limit. Include concurrent near-limit requests.
- [ ] Immediately lower the raw upload limit to one that safely fits base64/JSON and align the displayed message, Next parser, Go middleware, and storage validation.
- [ ] Decide from measured traffic whether presigned direct upload or streamed multipart is warranted. If not, keep the smaller simple contract.
- [ ] Store one canonical image until real durable transformations exist; provide backward-compatible reads/migration for existing `full/large/small` keys.
- [ ] Verify registration Turnstile and edge rate/body controls act before expensive downstream processing wherever the architecture permits.

### Task 21: Preserve custom price values and harden small form failures

**Findings:** WEB-02 and lower-risk web defects
**PR boundaries:** 21A price preservation; 21B corrupt local state/URI; 21C calendar embeds; 21D dialog accessibility
**Files:**

- Modify/Test: `web/src/components/forms/ProfileForm.tsx`
- Modify/Test: `web/src/pages/mentor/[slug]/contact.tsx`
- Modify/Test: `web/src/components/mentors/MentorsFilters.tsx`
- Modify/Test: calendar widgets and modal components

- [ ] Add a test rendering custom price `$75`, submit without touching it, and assert exact preservation.
- [ ] Render the current unknown value as an explicit option or use a text/datalist field; do not coerce it to `Free`.
- [ ] Wrap local-storage JSON parse and URI decode in narrow recovery that discards only corrupt input.
- [ ] Build calendar embed URLs with `URL`, add iframe titles, and test existing query strings.
- [ ] Add focus trap, initial focus, Escape close, and focus restoration tests for decline/username dialogs.
- [ ] Run targeted Jest tests, then `cd web && npm test -- --runInBand && npm run lint && npx tsc --noEmit`.

### Task 22: Bound catalog and OG rendering work

**Findings:** WEB-03, WEB-04
**PR boundaries:** 22A catalog projections/pagination; 22B OG bounds/cache
**Files:**

- Add API catalog/sitemap projections and repository queries
- Modify: `web/src/pages/index.tsx`
- Modify: `web/src/pages/mentors/index.tsx`
- Modify: `web/src/pages/mentors/[tag].tsx`
- Modify: `web/src/pages/sitemap.xml.ts`
- Modify/Test: `web/src/pages/api/og/mentor.tsx`

- [ ] Create representative large-catalog fixtures and failing budgets for rows fetched, response bytes, upstream calls, and render concurrency.
- [ ] Add cursor pagination and server-side tag filtering with a short public-card projection. Add a minimal sitemap projection.
- [ ] Use ISR/cached snapshots where freshness permits; keep filters accessible and URL-addressable.
- [ ] Restrict OG to GET/HEAD, validate slug bounds, ignore/canonicalize caller `v`, cache by mentor revision, cap image bytes and render concurrency.
- [ ] Test random cache-buster values: one mentor revision should cause at most one upstream fetch/render per cache lifetime.
- [ ] Remove the 10 MB page-warning increase after page-data budgets pass.

### Task 23: Repair source-map and build-secret handling

**Findings:** OBS-02, PRIV-01
**Files:**

- Modify: `web/next.config.js`
- Modify: `web/Dockerfile`
- Replace/Modify: `web/scripts/filter-sourcemaps.js`
- Modify: `.github/workflows/ci-web.yml`, `.github/workflows/deploy.yml`

- [ ] Add a build verification script that requires nonempty maps when either provider is enabled and resolves a known minified frame to the expected source line.
- [ ] Enable browser source maps independently of PostHog; upload to each configured provider before deletion.
- [ ] Replace mapping deletion with a source-map-aware source/content rewrite, or omit sourcesContent only if stack mapping remains valid.
- [ ] Pass upload credentials through BuildKit secret mounts or a separate post-build upload job. Never persist them as Docker `ARG`→`ENV` state.
- [ ] Make CI and local production builds use the same explicit contract; inspect image history/cache for sentinel secrets.

### Task 24: Split configuration validation by binary and cap request bodies

**Findings:** CFG-02, SEC-06
**PR boundaries:** 24A per-binary validation; 24B body limits; 24C URL validation; 24D Turnstile provenance
**Files:**

- Modify: `api/config/config.go`
- Modify: `api/cmd/api/main.go`, `api/cmd/worker/main.go`, `api/cmd/migrate/main.go`
- Modify: URL-bearing request models/validators
- Modify: `api/pkg/turnstile/turnstile.go`
- Add table-driven configuration and middleware tests

- [ ] Define `ValidateForAPI`, `ValidateForWorker`, and `ValidateForMigrate` with production/dev matrices for S3, SES, triggers/jobs, base URLs, TTLs, Discord, DB, JWT, and Turnstile.
- [ ] Test absent, partial, contradictory, insecure-production, and valid configurations. Each binary should require only what it uses.
- [ ] Add a small global JSON body limit and explicit larger image override; ensure worker/internal routes are capped too.
- [ ] Add an HTTPS-with-host validator and optional hostname allowlists; reject opaque schemes, credentials, and malformed hosts.
- [ ] Validate Turnstile HTTP status and configured hostname/action. Treat remote IP as optional defense-in-depth, not identity.
- [ ] Run `cd api && go test -race ./config ./internal/middleware ./pkg/turnstile ./...`.

### Task 24P: Minimize PII and secrets in logs, errors, and support data

**Findings:** PRIV-01
**PR boundaries:** 24P-1 application redaction; 24P-2 Alloy/support bundle; 24P-3 erasure evidence
**Files:**

- Modify/Test: `api/pkg/logger/`, `api/pkg/errortracking/`, email sender/recipient logs, registration logs, and migrate DSN logging
- Modify/Test: `web/src/lib/logger.ts`, `web/src/lib/api-proxy.ts`, `web/src/lib/go-api-client.ts`
- Modify: `infra/alloy/config.alloy`, `infra/docs/troubleshooting.md`
- Modify: `docs/runbooks/data-deletion.md`, legal/retention records where ownership approves

- [ ] Add sentinel capture tests for emails, passwords, DSNs, access keys, tokens, capability IDs, and upstream error bodies. Assert no sentinel reaches structured logs, Loki-bound output, PostHog error properties, or support artifacts.
- [ ] Remove raw recipient/registration emails or replace them with a stable one-way operational hash. Parse DSNs and replace the password field; never truncate an unparsed secret-bearing string.
- [ ] Sanitize database/upstream errors before telemetry while retaining typed error class, operation, and correlation ID. Do not log full Go upstream response bodies from the BFF.
- [ ] Replace `env | grep -v SECRET` diagnostics with a strict allowlist of safe version, health, resource, and configuration-presence fields. Add an Alloy redaction safety net without treating it as the primary control.
- [ ] Make the GDPR procedure executable with safe parameter binding and document evidence/retention handling for Loki, PostHog, SES, and historical image prefixes. Rehearse a mentor with more than two historical slugs.

### Task 25: Align database identity, deletion, and indexes

**Findings:** DB-02
**PR boundaries:** 25A identity decision/schema; 25B deletion policy/schema; 25C measured indexes; 25D migration policy
**Files:**

- Add one or more new forward migrations
- Modify: mentor/request/review repositories and models
- Add PostgreSQL integration and query-plan tests

- [ ] Write and approve the product rule for repeated applications before implementation: one account with application history (preferred) or one normalized email across all non-declined records. Stop this task if product ownership has not selected the rule.
- [ ] Add a concurrency test for two normalized-email registrations and make the database constraint the final arbiter.
- [ ] Write and approve whether client requests/reviews survive mentor deletion before implementation. Align foreign keys, nullable Go types, joins, GDPR runbook, and tests with that single policy.
- [ ] Add the partial login-token index and remove the redundant slug index in a new migration.
- [ ] Add a down migration for deterministic tag seed data only if deletion semantics are safe; otherwise adopt and test an explicit irreversible marker/policy.
- [ ] Run representative `EXPLAIN (ANALYZE, BUFFERS)` before adding catalog/request/cron candidate indexes. Commit only indexes with measured benefit.

## Wave 4 — Operational validation and maintainability

### Task 26: Add a real infrastructure merge gate

**Findings:** OPS-08
**Files:**

- Modify: `.github/workflows/checks.yml`
- Modify: `.github/workflows/ci-api.yml`
- Modify: `.github/dependabot.yml`
- Add scripts/config for actionlint, ShellCheck, Compose, migrations, Grafana, and asset parity

- [ ] Extend path detection for `.github/**`, `infra/**`, `api/migrations/**`, `grafana/**`, `brand/**`, and live runbooks.
- [ ] Gate shell syntax/ShellCheck, actionlint, JS/JSON/YAML parsing, dev/prod Compose expansion, service-env allowlists, migration up/down parity plus apply-all on ephemeral PostgreSQL, PostHog/Grafana validation, and brand source/copy parity.
- [ ] Grant SARIF upload only `security-events: write`; remove `-no-fail`/silent continuation for new high/critical gosec findings under an explicit baseline policy.
- [ ] Seed a temporary known violation in test fixtures or a self-test mode and prove every gate fails when its class is broken.
- [ ] Keep one stable required check for docs-only PR compatibility.

### Task 27: Make alerts reflect real service limits

**Findings:** OPS-09
**Depends on:** Task 3 container changes
**Files:**

- Modify: `infra/docker-compose.yml`
- Modify: `infra/alloy/config.alloy`
- Modify: `grafana/alerting/alert-rules.yaml`
- Modify: Grafana dashboards/README and tests

- [ ] Restore verified per-container cAdvisor metrics or remove the nonfunctional collector/rules. Add restart policy and least-privilege hardening if retained.
- [ ] Alert on a percentage of configured memory/CPU limits or thresholds below each actual limit; never use a 1 GiB threshold for 256–768 MiB containers.
- [ ] Scope `up`, `pg_up`, error, and latency expressions to OpenMentor namespace/service/instance. Avoid aggregates that hide one failed instance.
- [ ] Feed synthetic OpenMentor and unrelated series into rule tests; only intended series should page.
- [ ] Induce controlled 80% pressure and a stopped service in staging and confirm alert routing.

### Task 28: Consolidate repeated BFF and form mechanics conservatively

**Findings:** MAINT-02
**Depends on:** Tasks 1, 2, 21–23 behavior tests
**PR series:** 28A BFF helper in route batches; 28B form schema/fields; 28C auth plumbing; 28D request-list presentation
**Files:**

- Create: a small typed helper under `web/src/server/` for method/auth/upstream/error/cookie mechanics
- Modify: duplicated routes under `web/src/pages/api/`
- Extract: shared profile/registration schema and field groups
- Refactor: duplicated auth context state plumbing and request-list presentation

- [ ] Characterize 3 representative routes—public, mentor-authenticated, admin-authenticated—with tests for method, cookies, status/body mapping, headers, logging, and redaction.
- [ ] Implement the smallest typed helper that makes those three pass; keep route-specific request validation explicit.
- [ ] Migrate routes in batches of at most 5 and run tests after each batch. Do not hide authorization or capability handling in a generic callback.
- [ ] Extract shared profile validation/schema before shared UI fields. Preserve registration-only and edit-only behavior with focused tests.
- [ ] Share auth state-machine plumbing but keep separate mentor/admin permissions, endpoints, and context types.
- [ ] Re-run `jscpd`; require reduced high-change duplication, not a repository-wide percentage target.

### Task 29: Remove verified dead code and repair live documentation

**Findings:** MAINT-01, WEB-05, and lower-risk catalogue
**PR boundaries:** 29A Go lifecycle/dead code; 29B web dead code/static pages; 29C live documentation/artifacts
**Files:**

- Remove verified unreachable Go functions/metrics or wire their lifecycle intentionally
- Remove unreferenced web barrels, exports, scripts, state, and orphan lockfile
- Modify live runbooks/readmes/comments; do not modify immutable migration history
- Add lifecycle tests for components retained

- [ ] Run `deadcode -test ./...`, `knip`, `rg`, and TypeScript/Go compilation to reproduce the audit list before deleting anything.
- [ ] Delete unreachable error/logger/tracing wrappers, dead barrels/scripts/exports, unused filter state, orphan `infra/package-lock.json`, stale Azure block, and dead sentinel errors in small language-specific commits.
- [ ] For rate limiter, metrics ticker, and analytics worker, choose one lifecycle: wire `Stop/Close` into graceful shutdown with tests or remove the background mechanism. Do not leave exposed unused lifecycle methods.
- [ ] Update comments that claim old token/storage/tag behavior and fix live deployment/provisioning/mentor-migration guidance. Preserve `docs/migration/` unchanged.
- [ ] Convert static informational pages from SSR to static generation after preserving analytics behavior through client/CDN events.
- [ ] Run API lint/race tests, web lint/typecheck/tests/build, dead-code tools, and `git diff --check`.

## Final release acceptance

- [ ] The SEC-01/SEC-02 work in Tasks 1–3 is deployed and the affected credentials/tokens/telemetry data have been rotated, invalidated, or purged with an incident record.
- [ ] API: `cd api && make lint && go test -race ./... && go vet ./... && go mod verify && govulncheck ./...` passes with no unaccepted reachable high finding.
- [ ] Web: `cd web && npm ci && npm run lint && npx tsc --noEmit && npm test -- --runInBand && npm run build && npm audit --omit=dev` passes or has a time-bounded documented upstream exception.
- [ ] Infrastructure merge gates pass for both example environments; no container receives a forbidden key and no action uses a mutable tag.
- [ ] Concurrent auth, transition, job replay, process-kill, image-bomb, large-body, and metric-cardinality tests pass.
- [ ] A production-shaped staging deployment, forced rollback/roll-forward, stale-backup alert, full restore, and VM-loss rehearsal are completed and recorded.
- [ ] Production runtime credentials cannot administer PostgreSQL or Docker and cannot read another service's secrets.
- [ ] Dashboards show queue age/failures, backup freshness, real container saturation, and correctly scoped service health without PII/capability labels.
- [ ] The audit report is re-reviewed against the final code; each finding is marked fixed, accepted with owner/expiry, or superseded by verified evidence.
