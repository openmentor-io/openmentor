# Full Repository Audit — 2026-08-02

> **⚠️ Superseded — evidence only.** Use
> [`2026-08-remediation-plan.md`](./2026-08-remediation-plan.md) as the actionable plan. Verification
> corrected four findings below: **JOB-01** is not attacker-reachable (no retry mechanism exists);
> **OPS-06**'s mechanism is backwards (rollback fails *closed* at the migrate gate); **PRIV-01**'s
> DSN-mask leak is theoretical at every username in the repo; and **SEC-02**'s log and PostHog claims are
> refuted (both already mitigated — the OpenTelemetry `url.query` leak is real). Two **SEC-03** template
> citations are also wrong. Retained as a record of what was examined.

## Audit record

| Item | Value |
|---|---|
| Repository | `openmentor-io/openmentor-io` |
| Tracked files | 605 |
| Scope | Application, API, database, infrastructure, CI/CD, observability, runbooks, assets, tests, dependencies, and repository governance |
| Companion plan | [`docs/audit/2026-08-02-repository-audit-remediation.md`](./2026-08-02-repository-audit-remediation.md) |

## Executive summary

The repository is not ready to be treated as a hardened production system without remediation. Its ordinary engineering baseline is healthy—both applications compile, lint, and pass their current tests; SQL is parameterized; public HTML rendering is sanitized; authentication cookies and CORS are configured sensibly; and no committed production secret was found. Those strengths are real, but they coexist with several high-impact failures at trust boundaries and in asynchronous state handling.

The most urgent issue is infrastructure-wide secret overexposure. Six services receive essentially the complete production environment, so a compromise of the public frontend exposes the database superuser URL, backup and image-storage credentials, SES, Cloudflare, Grafana, worker authentication, JWT secrets, and build-time upload credentials. The public frontend shares the database network, making several of those credentials immediately usable. This defeats the intended service boundaries.

The next urgent issue is sensitive capability leakage. Exact OpenTelemetry probes proved that magic-link tokens and review capability identifiers enter trace attributes. The review identifier is also sent explicitly to PostHog and can enter raw request logs. Anyone with access to those systems can reuse those capabilities. This is an incident-response item, not merely a future hardening task.

Backend workflows contain three related integrity problems: login tokens are consumed with a read followed by a separate clear and fail open when clearing fails; worker callbacks can replay old state over newer state; and essential emails/events run in detached, non-durable goroutines. Combined, these permit multiple privileged sessions from one admin link, stale events that reopen or downgrade records, and silent loss or duplication of user-facing notifications.

Finally, the operational recovery story is not reliable enough. Backup failures are suppressed while health checks only verify that the backup process is alive; the production template configures the backup sidecar to restart-loop; restore and PostgreSQL-upgrade instructions depend on credentials and a checkout absent from the production VM; deployments can overlap; and automatic image rollback does not restore schema compatibility.

### Overall recommendation

1. Treat telemetry capability leakage and shared-secret exposure as immediate incident-response work.
2. Stop feature work long enough to make authentication consumption, worker state transitions, storage validation, backup freshness, and deployments fail closed.
3. Replace detached event delivery with a PostgreSQL-backed durable job/outbox design. Use an established library such as River; do not build a queue framework in this repository.
4. Improve data consistency and performance only after the trust-boundary fixes are deployed and credentials are rotated.
5. Keep the current monolith and single-VM deployment for now. The audit does not justify microservices or Kubernetes. Clear service contracts, least privilege, durable work, and tested recovery provide far more value with much less complexity.

## Priority and severity model

| Priority | Meaning | Expected response |
|---|---|---|
| P0 | Active exposure or a plausible path to broad compromise/data corruption | Start immediately; mitigate before normal feature work |
| P1 | High-impact exploit, persistent integrity risk, or broken recovery control | Schedule in the next hardening release |
| P2 | Material reliability, privacy, maintainability, or scaling defect | Fix in bounded follow-up releases |
| P3 | Low-risk debt or cleanup | Address opportunistically after behavior is covered |

The labels are remediation priorities, not automatic CVSS scores. Operational findings are ranked by likely business impact and recovery difficulty as well as attacker reachability.

## Prioritized findings

| ID | Priority | Finding | Primary consequence |
|---|---:|---|---|
| SEC-01 | P0 | Shared runtime environment gives six containers almost every production secret | One container compromise becomes database/cloud/observability compromise |
| SEC-02 | P0 | Magic links and review capabilities are exported to traces, logs, and analytics | Session creation and fraudulent review capability theft |
| SEC-03 | P1 | User-controlled values are inserted unescaped into SES HTML templates | Branded phishing and content injection in trusted email |
| AUTH-01 | P1 | Mentor/admin magic links are not atomically single-use | Multiple sessions from one link; failures leave links reusable |
| JOB-01 | P1 | Replayed worker callbacks can overwrite newer state | Reopen requests, downgrade/reactivate mentors, duplicate email |
| JOB-02 | P1 | Essential events and image work use lossy detached goroutines | Lost, stale, reordered, or duplicated user notifications |
| SEC-04 | P1 | Image decoding has no dimension or pixel budget | Unauthenticated memory/CPU exhaustion |
| CFG-01 | P1 | Optional S3 wiring is dereferenced after registration commits | Process crash and orphaned registrations |
| SEC-05 | P1 | Traefik/cAdvisor have daemon- or host-equivalent access without hardening | Container exploit can become VM takeover |
| DB-01 | P1 | API, worker, and migrations use the PostgreSQL bootstrap superuser | Application compromise gains administrative DB authority |
| OBS-01 | P1 | Unauthenticated dynamic paths create unbounded metric label values | Process and metrics-backend memory exhaustion |
| OPS-01 | P1 | Deployments and rollbacks can interleave | Mixed releases and invalid rollback points |
| OPS-02 | P1 | Public health-check failure does not fail deployment | Broken DNS/TLS/routing reports success |
| OPS-03 | P1 | Backup failure/freshness is not monitored | Stated recovery point can silently become false |
| OPS-04 | P1 | Production backup template and implementation contradict each other | Default production backup process restart-loops |
| OPS-05 | P1 | Restore/upgrade/rollback instructions cannot run on the deployed layout | Recovery fails when it is needed most |
| OPS-06 | P1 | Image rollback ignores schema/data compatibility | Old binaries can run against incompatible new schema |
| SUP-01 | P1 | Production-authorized GitHub Actions use mutable tags | Supply-chain change can obtain cloud/SSH authority |
| SUP-02 | P1 | Production SSH identity verification falls back to TOFU | First-connection MITM can intercept deployment authority |
| DEP-01 | P1 | Reachable and bundled dependency vulnerabilities remain | Known denial-of-service/file-read/image-processing exposure |
| AUTH-02 | P1 | JWT validation/session revocation is weaker than the issued contract | Cross-domain token acceptance and stale privileged sessions |
| DATA-01 | P1 | State changes, tags, and registration steps are not atomic | Partial success and contradictory state |
| DATA-02 | P1 | Contact permits hidden/inactive mentors and request transitions race | Unauthorized contact/calendar disclosure and lost updates |
| WEB-01 | P2 | Advertised image size cannot fit base64 JSON request limit | Valid-looking uploads fail; large bodies pressure memory |
| WEB-02 | P2 | Unknown saved prices silently become `Free` on unrelated profile saves | User data corruption |
| WEB-03 | P2 | Public catalog pages fetch and serialize the complete mentor catalog | Linear response growth and repeated client scanning |
| WEB-04 | P2 | OG image rendering is insufficiently bounded and cached | Avoidable render/fetch resource amplification |
| OBS-02 | P2 | Source-map pipeline is inconsistent and destroys mappings | Production errors cannot be reliably diagnosed |
| CFG-02 | P2 | Validation is incomplete and not specific to each binary | Healthy processes fail only when integrations are invoked |
| SEC-06 | P2 | General JSON body limits and URL-scheme validation are incomplete | Memory pressure and unsafe downstream links |
| PRIV-01 | P2 | Logs, telemetry, and support bundles collect avoidable PII/secrets | Privacy, erasure, and credential-disclosure risk |
| JOB-03 | P2 | Cron jobs and migration consumers lack leases/idempotent claims | Duplicate sends and duplicated migration side effects |
| DB-02 | P2 | Identity, deletion, indexes, and migration reversibility conflict | Ambiguous login, broken retained records, slower hot paths |
| OPS-07 | P2 | Local deployments can publish dirty code under an existing SHA tag | Rollback artifacts are not reproducible |
| OPS-08 | P2 | Infrastructure and migration changes lack a required validation gate | Broken operational changes can merge green |
| OPS-09 | P2 | Resource alerts are dead or incorrectly scoped | OOM/failure can occur without a meaningful page |
| WEB-05 | P3 | Static content is server-rendered for logging | Unnecessary Node load and reduced cacheability |
| MAINT-01 | P3 | Dead code, barrels, scripts, metrics, and stale docs remain | Larger cognitive surface and misleading contracts |
| MAINT-02 | P3 | BFF/form/auth code is duplicated in high-change areas | Fixes must be repeated and can drift |

## Detailed findings

### SEC-01 — Shared runtime environment destroys secret isolation

**Evidence.** `infra/docker-compose.yml:94-99,218-223,243-248,272-277,310-315,372-377` attaches `.env.runtime` to frontend, backup, migrate, backend, worker, and Alloy. `infra/deploy-remote.sh:61-68` and `infra/deploy.sh:525-544` generate that file by removing only the two image-tag lines from the complete production environment. `infra/.env.production.example` contains database owner credentials, backup and application S3 keys, SES credentials, Cloudflare DNS authority, JWT and worker secrets, Turnstile, Grafana Cloud credentials, and build-only PostHog/Faro credentials.

Mechanical Compose expansion produced 101–103 environment keys for each affected service. The public frontend has the database URL and network access to PostgreSQL. The backup process receives application/email/auth secrets; Alloy receives database and cloud-control credentials it does not need; the API receives backup and DNS credentials.

**Impact.** A frontend remote-code-execution vulnerability or dependency compromise becomes a practical path to database takeover, credential extraction, worker callback forgery, email abuse, DNS modification, backup access, and observability access. File permissions do not help because the values are deliberately injected into the process.

**Remediation.** Replace shared `env_file` usage with explicit, per-service allowlists. Keep deploy/build-only variables off the VM runtime entirely. Use Docker secrets or root-owned files for high-value values where the consumer supports file-based configuration. Introduce separate database roles as described in DB-01. After the new contract is deployed, rotate every credential that was visible outside its intended service.

**Acceptance.** A CI test renders Compose and compares each service's environment keys to a committed allowlist. From the running frontend container, `DATABASE_URL`, `POSTGRES_PASSWORD`, `BACKUP_AWS_SECRET_ACCESS_KEY`, `SES_SECRET_ACCESS_KEY`, `CLOUDFLARE_DNS_API_TOKEN`, `WORKER_AUTH_TOKEN`, JWT secrets, and `GCLOUD_RW_API_KEY` must be absent.

### SEC-02 — Sensitive capabilities are exported through observability

**Evidence.** `web/src/lib/tracing-server.ts:54-74` enables HTTP and Undici instrumentation without sanitizing URL attributes. In-memory exporter tests reproduced incoming `url.query=token=<secret>` and outgoing review calls with the capability in `url.full` and `url.path`. Browser cleanup in `web/src/pages/reviews/new.tsx:48-57` occurs after the server span exists. `web/src/lib/logger.ts:171-181` and `web/src/lib/with-observability.ts:75-90` log raw request URLs. `web/src/pages/mentor/[slug]/contact.tsx:263-274` sends `request_id` to analytics, while `web/src/lib/analytics.ts:76-146` does not block that key. `web/src/lib/posthog.ts` only scrubs IDs embedded inside URL strings.

The same capability reaches the Go layer. `api/cmd/api/main.go:372-375` puts `:requestId` in the review route path, which `otelgin` records as raw `url.path`. `api/internal/middleware/observability.go:51-87` logs the actual path and, for errors, raw route parameters. `api/internal/services/review_service.go:59-222` includes the request ID in application logs and analytics, and `api/internal/services/contact_service.go:106-134` also sends it to backend analytics. `api/pkg/email/templates/assets/session-complete.json` constructs the capability-bearing review URL.

Affected entry points include mentor/admin magic callbacks, mentor confirmation, `/reviews/new?request_id=`, and `/api/reviews/check?request_id=`. The review request ID is the credential accepted by review check/submission, not a harmless analytics identifier.

**Impact.** Trace, log, or analytics readers can recover login/confirmation tokens or submit a fraudulent review after satisfying Turnstile. Retention and replication turn a short-lived request secret into a broader incident surface.

**Remediation.** Immediately sanitize sensitive query keys and capability-bearing path segments before span creation/export and before logging in both services. For current web libraries, use HTTP `startIncomingSpanHook` and Undici `startSpanHook`, backed by one central sanitizer and an exporter-level defensive filter. Configure or wrap `otelgin` so Go spans never receive raw secrets; sanitize Go request/error context and remove capability values from both analytics trackers. Do not forward the replacement review token in a Go URL path or query. Carry it in a POST body or a dedicated secret header that is explicitly excluded from capture, while keeping a non-secret route template. Replace review authorization by primary key with a separate random, hashed, expiring, single-use token. Restrict and purge affected Tempo/Loki/PostHog data where supported; invalidate still-live login and confirmation tokens.

The configured HTTP header allowlist at `tracing-server.ts:63-67` did **not** export the internal auth header in the reproduced Undici path. Remove the hazardous latent configuration, but do not describe that header as a confirmed historical leak.

**Acceptance.** Web and Go in-memory exporter, logger, error-context, and analytics tests feed exact, nested, encoded, camel-case, and snake-case sentinel values. No raw value may occur in attributes, messages, properties, route parameters, or upstream URLs.

### SEC-03 — SES templates interpolate user-controlled HTML

**Evidence.** `api/pkg/email/sender.go:100-133` serializes `Message.Props` directly into SES `TemplateData`. Contact details, names, review content, profile fields, emails, calendar links, and moderator notes reach HTML/text or `href` contexts through jobs such as `job_new_request_watcher.go:92-139`, `job_process_review.go:84-91`, and `job_mentor_moderation.go:153-180`. Several templates interpolate values directly, including `new-request-calendly.json` and `new-mentor-returned.json`. Escaping in a few special flows confirms that callers currently bear an inconsistent responsibility.

[AWS documents that SES template replacement data is not HTML-escaped](https://docs.aws.amazon.com/ses/latest/dg/send-personalized-email-advanced.html).

**Impact.** An unauthenticated contact or review author can inject links, images, and misleading branded layout into email sent by OpenMentor. Most mail clients block scripts, but branded phishing and content injection remain practical.

**Remediation.** Create one typed rendering boundary that distinguishes escaped text, validated URL values, and narrow server-generated trusted HTML. Escape every user-controlled text value centrally; accept only `https` URLs from expected origins where links are necessary. Do not rely on each job author remembering to escape.

**Acceptance.** Submit closing tags, links, images, entity/encoding variants, and malicious URL schemes through every user-controlled field used by email. They must render as literal text or be rejected. Approved server-generated fragments must still render.

### AUTH-01 — Magic-link consumption is non-atomic and fails open

**Evidence.** Mentor verification (`api/internal/services/mentor_auth_service.go:184-230`) and admin verification (`admin_auth_service.go:147-182`) query by token, validate in application code, then clear with a second update. Both log a clear failure and still mint a session. Repository clear methods update by account ID rather than by the observed token.

**Impact.** Concurrent verification can issue multiple 24-hour sessions. A transient database failure leaves the link reusable while granting a session; administrator links make this a privileged-session issue.

**Remediation.** Consume with one `UPDATE ... WHERE login_token=$hash AND login_token_expires_at >= NOW() RETURNING ...` and mint a JWT only from the returned row. Add a partial unique mentor token index. Apply the same atomic pattern to email confirmation and resend, storing confirmation tokens hashed.

**Acceptance.** Two concurrent requests produce exactly one session. An injected database update failure produces no session. The old token fails after resend and confirmation emits exactly one transition/event.

### JOB-01 — Worker callbacks replay stale state

**Evidence.** `job_new_mentor_watcher.go`, `job_mentor_moderation.go`, and `job_new_request_watcher.go` call repository updates that are not tied to a unique event, expected version, or prior state. Examples include an old moderation callback intentionally changing the database to match its payload and the new-request job unconditionally restoring `pending`. A replay can therefore downgrade/reactivate a mentor, clear credential state, or reopen a completed request.

**Impact.** Duplicate/delayed delivery and misuse of the shared worker token can corrupt current state, restore old access, and send duplicate messages. SEC-01 currently exposes that token beyond the worker/API boundary, increasing the practical risk.

**Remediation.** Make consumers notification-only where the API has already committed the state. When a consumer must mutate state, require an event ID and an expected entity version/state, atomically claim the event, update with compare-and-swap, and require exactly one affected row.

**Acceptance.** Replay every event after advancing the entity to a later state; no state, credential, metric, or email may change. Duplicate event IDs are acknowledged without side effects.

### JOB-02 — Detached goroutines are not a delivery system

**Evidence.** `api/pkg/trigger/trigger.go:22-139` sends callbacks in detached goroutines, ignores empty destinations, and only logs failures. There is no persistence, retry, backpressure, ordering, or shutdown drain. `api/pkg/s3storage/storage.go:235-255` starts image work the same way. Authentication email events can be reordered after a newer token is stored; partial multi-recipient failures duplicate earlier sends on coarse retry.

**Impact.** A process restart after commit loses confirmation, login, moderation, request, and review work. Old login links can arrive last. Registration can report success while image work and notifications disappear.

**Remediation.** Adopt a PostgreSQL-backed durable queue/outbox using River. Persist domain change and outbox row in one transaction where consistency matters. Give each job a stable idempotency key, bounded retries with jitter, explicit terminal/dead-letter state, and metrics. Preserve cron for schedules but enqueue durable work. Drain workers on shutdown.

**Acceptance.** Tests terminate the process after transaction commit, replay deliveries, deliver out of order, and fail the third of multiple recipients. Restarting must complete outstanding work exactly once at the logical-effect level.

### SEC-04 and CFG-01 — Image validation and optional storage can crash the API

`api/pkg/imageclass/imageclass.go:60-150` performs full image decoding and border traversal without first bounding width, height, total pixels, or aspect ratio. The 10 MB body/base64 limit does not prevent a compressed image bomb. Storage also validates decoded byte size and magic bytes but not geometry.

Separately, `api/cmd/api/main.go:271-284` constructs the storage client only when both access keys exist, while `api/config/config.go:370-394` does not require storage configuration. Registration commits rows/tags and then unconditionally calls `UploadImageAllSizesAsync`; a nil receiver is dereferenced inside a goroutine beyond HTTP panic recovery.

**Remediation.** Use `image.DecodeConfig` before full decode, enforce conservative dimension/pixel/aspect budgets, and decode once for all classification/storage preparation. Make storage either a validated startup requirement or an explicit disabled feature that rejects image-bearing requests before mutation. Validate endpoint, bucket, region, and partial credential combinations.

**Acceptance.** Tiny compressed fixtures declaring extreme geometry fail quickly under a bounded memory test. Missing/partial storage configuration either stops startup deterministically or rejects the operation before any row/goroutine. Add decoder fuzzing.

### SEC-05 and DB-01 — Host and database privileges are excessive

`infra/docker-compose.yml:58-74` gives Traefik the Docker socket and gives cAdvisor `/var/run`, host root, sysfs, and Docker data. A read-only bind does not make Unix-socket API calls read-only. Neither service uses meaningful `read_only`, capability dropping, or `no-new-privileges` hardening.

`infra/docker-compose.yml:175-179` initializes `POSTGRES_USER=openmentor` as the image bootstrap superuser, and `infra/.env.production.example:106-121` gives that same role to migration, API, and worker processes.

**Remediation.** Put an allowlisted Docker socket proxy in front of Traefik, restrict cAdvisor mounts to its documented minimum, and harden both containers. Create separate database owner/bootstrap, migrator, runtime, backup, and monitoring roles. The runtime role should only use the application schema/tables/sequences and must not create roles/extensions, change ownership, or drop the schema.

**Acceptance.** Docker container-creation requests through the service-visible endpoint fail. API credentials cannot perform administrative database operations; migrations still run with the migrator identity.

### OBS-01 — Request-derived metrics have unbounded cardinality

`web/src/lib/with-observability.ts:24-41` replaces only complete UUID path segments, then allocates counter/histogram/gauge labels before authentication at lines 48-76. Next dynamic routes accept arbitrary non-UUID path values. Each unique value therefore creates new in-process and remote metrics series.

**Remediation.** Stop inferring route names from request paths. Supply a compile-time/static route template when wrapping each handler, or use framework route metadata proven not to contain parameter values. Keep client-derived values out of metric labels.

**Acceptance.** Thousands of unauthenticated requests with unique path parameters create a fixed number of series.

### OPS-01 through OPS-06 — Deployment, backup, and recovery controls are not fail-safe

- `.github/workflows/deploy.yml` has no production `concurrency`; local and CI tools share one live directory and one `.env.backup`; no remote lock exists.
- `.github/workflows/deploy.yml:301-321` and `infra/deploy.sh:650-667` warn and continue when the public endpoint is unhealthy or `DOMAIN` is missing.
- `infra/postgres-backup/backup.sh:163-169` suppresses backup failures. Deployment calls the sidecar healthy when its process is merely running. No freshness alert protects the stated recovery point.
- `infra/.env.production.example:123-140` sets a backup bucket with empty dedicated credentials and promises a fallback removed at `backup.sh:44-59`, causing a restart loop.
- Restore and PostgreSQL-upgrade runbooks call host AWS commands without credential mapping even though the VM intentionally has no AWS credentials. Other rollback/upgrade commands require a monorepo checkout although deployment syncs only `infra/`.
- Migrations run before application startup, while automatic rollback restores only image tags. Migrations 000005 and 000009 demonstrate data/schema changes that are not safely reversed.

**Remediation.** First add workflow concurrency and a shared remote `flock`; make the public probe mandatory with bounded retries; validate backup configuration before mutation; persist and alert on last successful off-site backup; and rewrite/rehearse recovery commands against a production-shaped scratch VM. Enforce expand/contract migrations and N/N-1 binary/schema compatibility. Prefer roll-forward; require a fresh tested backup and explicit operator decision for incompatible migrations. Do not automatically run down migrations.

Atomic versioned release directories are a useful later simplification, but they are not a prerequisite for the immediate lock and validation fixes.

**Acceptance.** Simultaneous deployments serialize. Broken public TLS/routing fails the release. Bad database and S3 backup credentials make freshness unhealthy and alert. Restore/upgrade runbooks execute verbatim on a VM containing only deployed artifacts. CI runs release N and N-1 binaries against schema N+1.

### SUP-01, SUP-02, and OPS-07 — Release provenance can be subverted

All GitHub Actions use mutable version tags, including jobs with OIDC and production SSH secrets; Dependabot does not manage the `github-actions` ecosystem. GitHub states that a full commit SHA is the only immutable action reference: [GitHub Actions secure-use reference](https://docs.github.com/en/actions/security-for-github-actions/security-guides/security-hardening-for-github-actions#using-third-party-actions).

CI falls back to `ssh-keyscan` over the connection it is trying to authenticate, and local deployment/rollback uses `StrictHostKeyChecking=accept-new`. Local deployment labels the current working tree with the `HEAD` commit SHA without requiring the tree to be clean or the commit pushed, so dirty code can overwrite a supposedly immutable rollback tag.

**Remediation.** Pin actions to reviewed full SHAs and enable GitHub Actions Dependabot. Minimize permissions by job and constrain the AWS role trust policy. Require an independently configured host key and fail closed. Refuse dirty/untracked relevant files and unpushed commits except via an audited break-glass flag; deploy full-SHA image digests and configure ECR immutable tags.

### DEP-01 — Current dependency and toolchain vulnerabilities

As of the audit date:

- `govulncheck` found reachable [GO-2026-6061](https://pkg.go.dev/vuln/GO-2026-6061) through `google.golang.org/grpc v1.81.1`; the fixed release is v1.82.1.
- The local Go 1.25.5 toolchain reported reachable standard-library advisories fixed across later 1.25 patch releases. The repository is inconsistent: `api/go.mod` declares 1.25.0 while CI and Docker use floating 1.26 references. [The current Go download feed](https://go.dev/dl/?mode=json) reported 1.26.5.
- `npm audit --omit=dev` reported three high production vulnerability paths. Next 16.2.11 bundles PostCSS 8.4.31 affected by [GHSA-qx2v-qp2m-jg93](https://github.com/advisories/GHSA-qx2v-qp2m-jg93), [GHSA-6g55-p6wh-862q](https://github.com/advisories/GHSA-6g55-p6wh-862q), and [GHSA-r28c-9q8g-f849](https://github.com/advisories/GHSA-r28c-9q8g-f849), and sharp 0.34.5 affected by [GHSA-f88m-g3jw-g9cj](https://github.com/advisories/GHSA-f88m-g3jw-g9cj). Direct root versions are patched, but the nested copies remain.

The PostCSS paths require processing attacker-controlled CSS, which the current product does not intentionally do. Most normal UI images bypass Next optimization, reducing sharp exposure, but image input still deserves conservative treatment. These qualifications lower immediate exploitability, not the need to remove known vulnerable production code.

**Remediation.** Update gRPC. Pin one supported patched Go toolchain—currently 1.26.5—across `go.mod`, CI, and Docker by full patch version/digest. For Next's nested packages, test controlled `overrides` to PostCSS >=8.5.18 and sharp >=0.35.0 or take the first upstream Next release with corrected ranges; do not accept npm's suggested framework downgrade. Exercise production build, image rendering, and optimization paths.

### AUTH-02 — JWT validation and revocation are incomplete

`api/pkg/jwt/jwt.go:65-116` issues HS256 with an issuer but validates any HMAC algorithm and does not require the configured issuer, audience, expiration, issued-at, or subject consistency. Missing claims can yield a recovered 500. Admin and mentor middleware trust claims for the token lifetime; logout only clears one browser cookie, and role/status changes do not revoke issued sessions.

**Remediation.** Require HS256 exactly, issuer, audience, expiry, issued-at, subject UUID, and token type. Use separate admin and mentor signing keys or audiences. For admin requests, re-read current role/session version or use revocable server-side sessions; for mentor mutation, verify current allowed status. Avoid a generic session platform—one indexed session/version lookup is enough at this scale.

### DATA-01 and DATA-02 — Multi-step writes and transitions are inconsistent

- `profile_service.go:100-148` commits mentor fields before tags and can ignore tag failure; registration creates the mentor before tags/image/event work.
- Picture upload writes three objects before database metadata updates and can report success after metadata failure.
- Contact inserts a request before fetching the mentor, uses hidden/non-public options, and can return an inactive mentor's calendar URL.
- Mentor/admin request transitions read a state and later update by ID, so conflicting actions can both validate and both emit events.
- Confirmation lookup/transition and review eligibility/insert follow the same check-then-write shape.

**Remediation.** Transactionally group relational field/tag changes. Validate tags and image bytes before creating a registration. Use `INSERT ... SELECT` gated on active/visible mentor state. Encode owner and expected state/version in update SQL and require one returned row; put corresponding outbox work in the same transaction. Preserve typed conflict/not-eligible errors.

### WEB-01 and image storage complexity

The UI accepts 10 MiB raw images, then embeds base64 in JSON. The Next body and Go request limits are also 10 MiB, so files above roughly 7.5 MiB pass client validation but deterministically exceed the encoded request limit. Large public registration bodies are parsed before backend Turnstile validation. `api/pkg/s3storage/storage.go:186-236` decodes repeatedly and uploads identical bytes under `full`, `large`, and `small`, tripling object requests/storage without producing thumbnails.

**Remediation.** Immediately align the raw limit to the serialized contract and add concurrency/edge protection. Then move to presigned direct or streamed multipart uploads if image volume justifies it. Decode/validate once. Until real variants are generated by a durable established image processor, store one canonical object rather than three aliases.

### WEB-02 — Unknown prices silently become `Free`

Price is stored as free text, but `web/src/components/forms/ProfileForm.tsx:421-432` renders a finite uncontrolled select. A reproduced render with `defaultValue="$75"` selected `Free`; an unrelated save can therefore overwrite legacy/custom data.

**Remediation.** Preserve unknown values as a visible option or restore a free-text/datalist input. Test an untouched custom value round trip.

### WEB-03 and WEB-04 — Public rendering does not have resource budgets

Homepage, mentor listing, tag pages, and sitemap fetch the full mentor catalog, often with long fields, then filter repeatedly in the browser. `next.config.js` raises the page-data warning to 10 MB rather than constraining growth. The OG endpoint performs upstream/image fetches and a 1200×630 render without method restriction, slug bounds, concurrency control, or application caching; arbitrary `v` values multiply cache keys.

**Remediation.** Add API projections and cursor pagination/tag filtering, use ISR/cached catalog snapshots, and give sitemap a minimal projection. For OG images, permit GET/HEAD, validate slug, canonicalize/ignore `v`, cache by mentor revision, and cap rendering concurrency/input bytes. Do not introduce a separate search service until measured catalog scale requires it.

### OBS-02 — Source-map pipeline cannot meet its purpose

Faro source-map upload depends indirectly on PostHog enabling maps; PostHog can delete them before Faro runs; `web/scripts/filter-sourcemaps.js:64-75` clears mappings. A production build without PostHog credentials generated no browser maps. CI and local production builds also pass different credential contracts.

**Remediation.** Generate maps independently, use BuildKit secret mounts or a post-build upload job, upload to all configured providers before deletion, and rewrite maps with a source-map-aware library. Verify that a known minified production frame maps back to source and that no secret appears in image history/cache.

### CFG-02 and SEC-06 — Runtime contracts are incomplete

Global API validation omits S3, SES, trigger URLs, base URL/TTL constraints, Discord, and cross-setting consistency, while worker/migrate can be forced to provide unrelated API settings. Most JSON endpoints and worker endpoints lack a global small body cap. Model `url` validation accepts non-HTTP schemes that can later become links. Turnstile checks success but not HTTP status or optional hostname/action provenance.

**Remediation.** Implement `ValidateForAPI`, `ValidateForWorker`, and `ValidateForMigrate`; fail startup on missing production contracts. Apply a small global JSON limit with explicit image overrides. Require HTTPS URLs with host and optional origin allowlists. Validate Turnstile hostname/action when configured, using [Cloudflare's server-side validation guidance](https://developers.cloudflare.com/turnstile/get-started/server-side-validation/).

### PRIV-01 — Logging and support collection exceed operational need

Raw registration/recipient emails enter Go logs and Loki; PostHog error capture receives raw error strings; the migration DSN mask can expose part of a password. The troubleshooting bundle runs `docker exec ... env | grep -v SECRET`, which still includes database URLs/passwords, worker token, access-key IDs, API keys, and many other sensitive values. The GDPR runbook cannot account for all historical image prefixes and lacks concrete Loki/SES handling.

**Remediation.** Centralize structured redaction/hashing, sanitize errors before telemetry, parse and replace DSN passwords, and make support diagnostics a strict allowlist. Add defense-in-depth Alloy redaction. Establish retention and deletion evidence for logs/email providers and complete the already-recorded DPA/region/LIA legal actions.

### JOB-03 — Scheduled and migration work can execute more than once

Cron lacks local overlap guards and distributed leadership; multiple worker replicas and manual endpoints can run the same task. Migration consumers select pending work without atomically claiming a lease. Image migration may trust existing destination size without content verification.

**Remediation.** Use `cron.SkipIfStillRunning`, PostgreSQL advisory locks/leases, durable send markers, and atomic `UPDATE ... RETURNING` or `FOR UPDATE SKIP LOCKED` claims. Store attempt/lease metadata and verify image SHA-256 after copy.

### DB-02 — Schema and repository contracts conflict

Confirmed issues:

- mentor login-token lookup has no partial index; moderator tokens do;
- `mentors_slug_idx` duplicates the index already created by `slug UNIQUE`;
- active-only email uniqueness permits multiple live draft/pending identities and lookup has no deterministic tie-break;
- `client_requests.mentor_id ON DELETE SET NULL` conflicts with non-null Go string scanners and inner joins;
- migration `000002_populate_tags.up.sql` has no down file, while other downs are explicitly lossy;
- likely catalog/request/cron indexes are query-aligned candidates, but must be justified with production-like `EXPLAIN ANALYZE` rather than added speculatively.

**Remediation.** Separate account identity from applications or enforce one normalized non-declined email. Decide whether deleted mentors' requests are retained/anonymized or deleted, then align nullability and joins. Add the token index and remove the redundant slug index in a new migration; historical migrations are immutable. Adopt an explicit irreversible-migration policy and test full migration application on ephemeral PostgreSQL.

### OPS-08 and OPS-09 — Operational code is under-tested and monitoring is misleading

Required checks only select `web/**` and `api/**`, so workflows, Compose, scripts, migrations, Grafana, and brand copies can change without relevant validation. API gosec uses non-gating output and SARIF upload can fail silently without scoped permission. Container CPU/memory alerts depend on series documented as absent; the 1 GiB memory threshold exceeds every configured container limit. Several alerts aggregate unscoped shared-stack series.

**Remediation.** Add an infrastructure gate for Compose expansion, shell syntax/ShellCheck, actionlint, JS/JSON/YAML validation, migration apply/parity, Grafana validation, and asset-copy checks. Give SARIF the required permission and gate new high/critical findings. Fix metrics collection before alert rules; alert on percentage of service limits and scope every query to the OpenMentor namespace/service.

### Lower-risk defects and cleanup catalogue

These are verified but should not distract from P0/P1 work:

- Static `about`, `faq`, `donate`, `privacy`, and `terms` pages use SSR mainly for logging. Convert them to static generation and client/CDN observability.
- Corrupt contact-form local storage and malformed URI fragments can throw during rendering; recover and discard invalid values.
- Calendar iframe query construction and accessible titles are wrong; two modals do not trap/restore focus.
- Review proxy validation can throw on non-string optional fields; several BFF routes collapse useful upstream 4xx/429 errors into 500.
- `NEXT_PUBLIC_GO_API_URL` exposes an internal topology value, image loader can construct `https://undefined/...`, and health checks generate routine logs.
- CSP permits `unsafe-inline`; retain only while framework/provider constraints require it and move toward nonce/hash-based script policy after measuring breakage.
- Compose expands host `${HOSTNAME}` to blank for service IDs; use explicit instance IDs. cAdvisor lacks a restart policy.
- Restore/provisioning/troubleshooting, migration, and brand guidance contains stale or dangerous commands. Update only live runbooks; preserve `docs/migration/` as historical evidence.
- Grafana's monitoring role uses `pg_read_all_data`, exposing PII. Disable high-privilege collectors or replace this with least-privilege views/grants.
- The single VM remains a documented availability risk. Keep it for current scale, but rehearse complete VM-loss recovery and use external uptime monitoring.

## Dead code and unnecessary complexity

### Verified unreachable or unused code

`deadcode -test ./...` reported these production functions unreachable:

- `api/internal/middleware/rate_limit.go:52` — `RateLimiter.Stop` (either wire all limiter lifecycle into shutdown or remove the stop machinery);
- `api/pkg/errors` — `AccessDeniedError`, `InternalError`;
- `api/pkg/logger` — `LogError`;
- `api/pkg/tracing` — `Tracer`, `StartSpan`.

Additional verified dead/stale surfaces:

- `web/src/config/index.ts`, `web/src/lib/index.ts`, and `web/src/server/index.ts` are unreferenced barrels;
- `dev2` and `start2` package scripts are dead;
- unused exports include several Faro/observability helpers, formatting utilities, Wordmark/Wysiwyg exports, and admin error helpers; remove exports only after direct reference checks;
- unused mentor-filter state (`selectedNoSessions`, `selectedNewMentor`) and duplicated component interfaces;
- API's `MentorProfileViews` metric is never incremented, `ErrReviewRequestNotDone` is never returned, one service stores unused configuration, and a commented Azure-era goroutine remains in `profile_service.go`;
- `infra/package-lock.json` is an empty orphan; a design document links to a missing brief; several active runbook comments contradict current code.

### Duplication measured, not guessed

`jscpd` analyzed 295 files / 45,886 source lines and found 94 duplicate blocks (3.23% duplicated lines overall). TypeScript is the main concentration (9.56% duplicated lines); Go duplication is low (1.45%) and often reflects clear domain parallels.

High-value consolidation targets:

1. Approximately 37 Next API routes repeat method gating, cookie/auth forwarding, proxy invocation, and error mapping. Introduce one small, typed BFF route factory/helper while preserving route-specific validation.
2. Admin and mentor auth contexts repeat session/bootstrap logic. Share the state-machine plumbing, not the domain permissions.
3. `ProfileForm` (roughly 650 lines) and `RegisterMentorForm` (roughly 1,000 lines) duplicate fields and validation. Extract shared schema and field groups before changing behavior.
4. Active/past mentor request pages repeat presentation and data-state branches. Share list/view components once their behavior is covered.
5. API mentor/admin authentication repeats token generation/verification. Consolidate the cryptographic consume primitive while retaining distinct roles, signing contracts, and responses.

Do **not** create a general repository framework or merge clearly separate mentor/admin domain services merely to reduce a small duplication percentage.

## Verification and baseline

| Check | Result |
|---|---|
| API lint (`golangci-lint`) | Passed, 0 unsuppressed issues |
| API race tests | Passed |
| API vet / module verification | Passed |
| API binaries | Built successfully |
| API coverage (`-coverpkg=./...`) | 37.6% statements; JWT, concurrency, configuration, and failure paths are especially weak |
| Web lint / TypeScript | Passed |
| Web tests | 35 suites, 283 tests passed; one React `act()` warning remains |
| Web coverage | 60.0% statements, 51.4% branches, 46.58% functions, 61.5% lines |
| Web production build | Passed after network access for Google font download |
| Compose dev/prod expansion | Passed; three blank `HOSTNAME` warnings |
| Shell/JS/JSON/YAML/SVG syntax | Passed |
| PostHog dashboard validation | Passed: 6 dashboards / 54 insights |
| Secret history scan | No verified tracked production secret; three synthetic/test/prose matches only |
| `govulncheck` | Reachable gRPC and local standard-library advisories as recorded in DEP-01 |
| `npm audit --omit=dev` | Three high production paths, zero critical |
| `deadcode` / `knip` / `jscpd` | Results incorporated above |

Coverage numbers are not quality scores. The critical gap is that security and failure semantics—token races, queue replay, storage misconfiguration, telemetry redaction, profile partial writes, recovery scripts—have little or no meaningful regression protection. Some API tests reconstruct production logic in test-local mocks instead of calling real services, which overstates their value.

## Limitations

- No production VM, live cloud account, live PostgreSQL data volume, Grafana/PostHog tenant, DNS zone, SES account, or object store was inspected. Configuration correctness in those systems remains unverified.
- Ignored real environment files were identified but deliberately not read. The audit evaluated tracked contracts and example configuration without exposing local secrets.
- Migrations were statically inspected; a cached PostgreSQL image was unavailable for a complete up/down execution during the audit. The remediation plan makes this a required CI check.
- The production build required network access to fetch Google fonts. This is a reproducibility concern; self-host the font inputs used by `next/font` if offline builds are required.
- Dependency results are time-sensitive and should be re-run when remediation begins.

## Verified strengths to preserve

- No verified committed production credentials, private key, SQL injection, path traversal, or user-controlled SSRF was found.
- SQL values are parameterized and dynamic update columns are allowlisted.
- Public rich HTML and JSON-LD rendering use focused sanitization/escaping; no frontend stored-XSS path was reproduced.
- Session cookies are HttpOnly, Secure by default, and SameSite Lax. Authentication-request responses resist account enumeration.
- CORS rejects wildcard origins with credentialed requests; worker/internal token comparison is constant-time.
- Public PostgreSQL ports are closed, the database volume is external to Compose ownership, and ECR access uses short-lived OIDC/local credentials rather than persistent VM registry keys.
- Slug mutation is carefully normalized and transactionally protected with uniqueness, locks, cooldown, and history.
- Migration constraints, foreign keys, deterministic seed IDs, and explicit warnings around lossy migration 000009 are generally thoughtful.
- Web sanitization, dynamic path encoding, cookie forwarding, security headers, strict TypeScript, reduced-motion/focus styling, and non-root runtime are solid foundations.
- Brand assets and served copies are currently synchronized and valid.

## Explicit non-recommendations

- Do not split the application into microservices or introduce Kubernetes to solve configuration and queue defects.
- Do not write a custom durable queue; use a maintained PostgreSQL-backed library.
- Do not edit already-applied historical migrations. Add new migrations and document irreversible boundaries.
- Do not automatically run lossy down migrations during application rollback.
- Do not create one highly generic BFF/domain framework. Extract only repeated stable mechanics behind typed, tested helpers.
- Do not add speculative indexes without representative `EXPLAIN ANALYZE` evidence.
- Do not collect more logs or telemetry as a substitute for durable state and tested recovery.
