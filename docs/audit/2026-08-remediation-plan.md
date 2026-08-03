# openmentor.io — Remediation Plan (2026-08)

**Status:** Authoritative and self-contained. You need no prior context to work from this document.
**Baseline commit:** `9b63e73`
**Verified:** 2026-08-03, by executing code against a running dev stack — see
[`verification/README.md`](./verification/README.md) to reproduce any finding yourself.
**Corrections to this document itself are in [§4.1](#41-corrections-to-this-plan-itself-round-2-review).
Read it before you act on any step — two of the instructions it corrects would have damaged live data.**

---

## Contents

1. [How to use this document](#1-how-to-use-this-document)
2. [System orientation](#2-system-orientation)
3. [Operating constraints](#3-operating-constraints)
4. [Where this plan came from, and what to distrust](#4-where-this-plan-came-from-and-what-to-distrust)
5. [Two failure chains worth understanding first](#5-two-failure-chains-worth-understanding-first)
6. [Phase 0 — diagnostics (run before changing code)](#6-phase-0--diagnostics-run-before-changing-code)
7. [Phase 0 — work items P1–P16](#7-phase-0--work-items-p1p16)
8. [Phase 1 — hardening (H1–H14)](#8-phase-1--hardening-h1h14)
9. [Phase 2 — correctness and performance (C1–C12)](#9-phase-2--correctness-and-performance-c1c12)
10. [Phase 3 — maintainability (M1–M7)](#10-phase-3--maintainability-m1m7)
11. [Decision gates needing the owner](#11-decision-gates-needing-the-owner)
12. [Explicitly out of scope](#12-explicitly-out-of-scope)
13. [Appendix: finding index](#13-appendix-finding-index)

---

## 1. How to use this document

Each work item is a self-contained card:

- **ID / title / severity / effort** — effort is XS (minutes), S (<½ day), M (1–2 days), L (3+ days).
- **Where** — exact files and line numbers as of `9b63e73`. Treat line numbers as starting points;
  they drift. The surrounding code descriptions are the reliable part.
- **Evidence** — what was actually observed. `[MEASURED]` means a number came from running code.
- **Why it matters** — the user-visible or security consequence.
- **Fix** — concrete steps. Where a judgement call exists, it says so.
- **Acceptance** — how you know you are done, phrased so it can be tested.
- **Regression test** — what to add so it cannot come back.

**You are expected to exercise judgement.** These cards describe intent and constraints, not
keystrokes. If you find a cleaner fix that satisfies the acceptance criteria, take it — and note the
deviation in your PR. Two things are *not* negotiable: the acceptance criteria, and the constraints in
§3 (they exist because the system is live).

**Verify before you fix.** Every P-item can be reproduced in a local dev stack in minutes using
[`verification/README.md`](./verification/README.md). Do that first — it tells you when you are done,
and a couple of these findings are subtle enough that fixing blind risks fixing the wrong thing.

**Working agreements** (from the repo's `CLAUDE.md`):
- One PR per work item unless a card says otherwise. Do not mix a security fix with cleanup.
- Cross-cutting changes (API contract, env vars, compose services) land as **one** commit touching all
  affected directories — that is the point of the monorepo.
- Product/architecture decisions get a row in `docs/migration/DECISIONS.md`.
- `docs/migration/` is a historical record of the getmentor→openmentor fork. Do not "fix" old paths there.
- Verify with `make ci` from `api/`, and `npm run lint && npm test && npx tsc --noEmit` from `web/`.
- Never commit real `.env` files. Never print secret values into logs, tests, or CI output.
- Create a branch per item; do not merge to `main` without explicit permission.

---

## 2. System orientation

A monorepo for an open-source mentorship platform (a fork of getmentor.dev).

| Directory | What it is |
|---|---|
| `web/` | Next.js 16 frontend, **Pages Router only** (no `app/`), React 19, TypeScript strict, Tailwind 3, Jest. Acts as a BFF: all data flows through `web/src/pages/api/*` proxies to the Go API. |
| `api/` | Go backend, module `github.com/openmentor-io/openmentor/api`. Three binaries: `cmd/api`, `cmd/worker`, `cmd/migrate`. Gin, pgx/v5, viper, zap, JWT, S3, SES v2, PostHog, OpenTelemetry. |
| `infra/` | Docker Compose + Traefik single-VM deployment, plus Grafana Alloy observability and a Postgres backup sidecar. |
| `docs/` | Decisions (`docs/migration/DECISIONS.md`), runbooks, design reference. |
| `grafana/` | Dashboards and alert rules (Grafana Cloud, not self-hosted). |
| `brand/` | Brand asset pack. Never redraw the logo; reference files verbatim. |

**Request path:** browser → Traefik (only service publishing :80/:443) → Next.js frontend →
`/api/*` BFF proxy → Go API → Postgres. The worker is internal-only, triggered by cron and by
fire-and-forget HTTP calls from the API.

**Two facts that explain several findings:**

1. **All API traffic arrives from the BFF's single source IP.** Any IP-keyed rate limiter therefore
   behaves as one global bucket. The login limiters were already re-keyed by email for this reason
   (`api/cmd/api/main.go:101-105`); others were not (see `P11`).
2. **Everything shares one flat Docker bridge network** (`openmentor-network`,
   `infra/docker-compose.yml:388-391`), and every service receives the full production environment
   file. So the internet-facing frontend holds database credentials *and* can reach Postgres (see `P10`).

---

## 3. Operating constraints

**The application is live and serving real traffic** (modest volume). A wider-audience launch event is
still ahead. This shapes the whole plan:

- **Short, planned downtime is acceptable. Extended downtime is not.** Nothing in Phase 0 needs more
  than a container restart, except a possible data restore (see `D2`).
- **Phase 0 migrations must be additive only** — add nullable columns and indexes; never rename or
  drop. Reason: `infra/rollback.sh` is inoperable across a migration boundary (it fails at the migrate
  gate and leaves the *bad* version live — see `H10`). Until that is fixed, every deployed image must
  be able to run against the previous *and* next schema.
- **Build indexes with `CREATE INDEX CONCURRENTLY`** so you do not lock writes. That cannot run inside
  a transaction, so it needs its own migration file with transactions disabled.
- **Sequence deploy-safety fixes before risky ones.** `P15` (mandatory health probe) and `P9` (backup
  sidecar restart loop) must land *before* `P10`'s credential rotation, or a deliberate maintenance
  restart will trigger an automatic rollback.
- **Some data may already be damaged.** Run the §6 diagnostics before changing code.
- **Credential rotation is mandatory, not optional** (`P10`), because secrets have been present in a
  live internet-facing container. It must be sequenced: fix the contract → deploy → rotate.

---

## 4. Where this plan came from, and what to distrust

Three inputs were merged:

1. An **external advisor's audit** — `docs/audit/2026-08-02-full-repository-audit.md` and its companion
   `2026-08-02-repository-audit-remediation.md`. Ran `deadcode`, `knip`, `jscpd`, `govulncheck`,
   coverage, Compose expansion, and in-memory OpenTelemetry exporter probes. Strongest on trust
   boundaries, state integrity, and operational recovery.
2. An **internal audit** — `docs/audit/2026-08-pre-launch-audit.md`. Strongest on CI/CD injection, a
   specific broken login path, rate-limiter behaviour under the real deployment topology, and repo hygiene.
3. **Empirical verification** — every P-item below was reproduced by running code.

Those three documents remain in the repo as evidence. **If you read them, know that they contain
claims that verification overturned.** Do not act on these:

| Claim in a source document | Reality |
|---|---|
| Internal audit "B4": the internal API auth token is exported into OpenTelemetry spans | **False.** `headersToSpanAttributes` is configured under `@opentelemetry/instrumentation-http`, but `web/src/lib/go-api-client.ts:104` uses global `fetch` (undici), which never enters that code path. Undici instrumentation is enabled but its own header capture is unconfigured. It is a **latent** misconfiguration — delete the block (`P14`), but nothing leaked. |
| Internal audit: "JWT is algorithm-confusion guarded" | **Imprecise.** `api/pkg/jwt/jwt.go:98` checks the HMAC *family*, not HS256 specifically. RS256→HS256 confusion is blocked, so it is not critical, but issuer and audience are never verified (`H2`). |
| Internal audit: `normalizeRoute` bounds metric cardinality | **Wrong.** [MEASURED] 500 unique non-UUID path values produced 500 distinct metric series (`P13`). |
| External audit "JOB-01" (P1): replayed worker callbacks can overwrite newer state | **Mechanics real, exploitability overstated.** The worker has no published ports, is token-authenticated with a constant-time compare, and **no retry mechanism exists** (`trigger.CallAsync` is single-shot). Realistic trigger is two concurrent moderator actions. Treat as correctness (`H3`), not urgent security. |
| External audit "OPS-06": rollback lets old binaries run against a newer schema | **Backwards.** golang-migrate's `versionExists` check makes it fail *closed* at the migrate gate. The real defect is that `rollback.sh` is inoperable across a migration boundary and fails leaving the bad version live (`H10`). Already documented at `infra/DEPLOYMENT.md:142-188`. |
| External audit "PRIV-01" (part): the migration DSN mask leaks part of a password | **Theoretical.** `maskDatabaseURL` is a blind 20-byte prefix; `postgres://` is 11 chars, so with the username `openmentor` (every template in the repo) zero password bytes leak. Only leaks if the username is shortened below 8 characters (`C11`). |
| External audit "SEC-02" (part): magic-link tokens leak to logs and PostHog | **Refuted for those two sinks.** Logs: `logHttpRequest` is only reached from API-route wrappers, the callback pages have no `getServerSideProps`, verification carries the token in a POST body, and Go redacts `token` (`api/internal/middleware/observability.go:15-18`). PostHog: `web/src/pages/mentor/auth/callback.tsx:29-34` strips the token before anything fires. The OpenTelemetry `url.query` leak **is** real (`P14`). |
| External audit "SEC-03": `new-mentor-returned.json`'s `href` and `{{request_url}}` are injectable | **Wrong for those two** — both are server-built. The real hole in that template is `{{reviewer_note}}` in the body (`P6`). |
| External audit: the 8 auth BFF routes collapse 4xx into 500 | **No.** They hand-roll equivalent logic inline because they need a different error envelope. Exactly **four** routes are broken (`P7`). |

Two further calibrations, so you size the work correctly:

- **`P6` (email injection) is HTML injection into email — phishing from a trusted sender.** It is not
  stored XSS in the app and not code execution; mail clients strip `<script>`. Serious for brand and
  credential-phishing reasons; do not escalate it beyond that.
- **`P7`'s Turnstile failure self-heals after ~5 minutes** (the widget auto-refreshes on token expiry).
  Every retry a user would realistically make still fails, but "requires a page reload" is inexact.

### 4.1 Corrections to this plan itself (round-2 review)

The table above corrects the **source audits**. This section corrects **this document**. A second
review pass over the round-1 fixes found five wrong or unsafe statements here. Where following the
text would have caused harm, the text was changed in place; what it originally said is recorded below
so the audit trail survives.

| # | Where | What this plan originally said | Reality |
|---|---|---|---|
| 1 | `D1` repair | "run finalization for each row via the worker's manual trigger endpoint (`POST /jobs/...`, idempotent)" | **Unsafe, and the handler is not idempotent.** `NewMentorWatcher` rewrites `status` unconditionally, mints a *new* confirmation token (killing the link already in the mentor's inbox), re-randomizes `sort_order` and sends another email. `D1` also contains imported (`inactive`), `active` and just-committed rows: replaying an `active` row makes it count *itself* as a duplicate and sets `declined`. Now points at the `D1b` classification, the 15-minute age cutoff, the duplicate check and the immediate re-check in `data-repair.md` §D1. |
| 2 | `D3` query | A query covering `client_requests.description`/`name`, `mentors.name`/`calendar_url` and the review bodies, with a `\b` word boundary | **Incomplete, and one branch never matched.** It missed `client_requests.preferred_contact` (`{{mentee_contact}}`) and `mentors.price` (`{{request_price}}`) — both reach signed mail unescaped — so a clean `D3` proved nothing about them. In PostgreSQL's regex flavour `\b` is BACKSPACE, so the tag-name branch silently returned zero rows. Now synced with `diagnostics.sql` D3, which is the maintained copy. |
| 3 | `P2` | "three `ScanMentor` queries select `sort_order` raw" | **Four.** `FetchAllMentorsFromDB` (`mentor_repository.go:513`, the public catalog) was missed. Fixing three leaves one NULL able to break the whole catalog listing. The implementation fixes all four. |
| 4 | `P4` acceptance | "`experience` (same uncontrolled shape, closed option set) is unaffected" | **Wrong.** `experience` (`ProfileForm.tsx:404-409`) has the identical uncontrolled-`<select>` corruption path and silently rewrites any off-list value to `2-5`. `diagnostics.sql` D2d counts it; the fix must cover both fields. |
| 5 | `P6` step 4 | "wrap the two intentional server-generated fragments in `template.HTML` … remove their now-redundant manual escaping" | **Doing that literally recreates the injection.** `html/template` does not inspect a `template.HTML` value, so a concatenated fragment carrying `DeclineComment`/`reviewer_note` would ship raw markup with the escaping removed. The fragments must be *rebuilt as `html/template` templates* so inner values are escaped structurally. The implementation does this (`declineInfoTpl` + `renderFragment`); only this plan's instruction was wrong. |

Items 3, 4 and 5 are this plan being wrong about code that is already correct. Items 1 and 2 are
operator instructions that were dangerous or useless as written.

---

## 5. Two failure chains worth understanding first

These explain why several items are grouped and ordered the way they are.

### Chain A — a registration can permanently lock a mentor out of their own account

Each step verified by execution:

1. `RegisterMentor` commits the mentor row with `status='draft'` and `sort_order = NULL`
   (`api/internal/services/registration_service.go:150-170`).
2. Finalization is dispatched via `trigger.CallAsync` (`:203`) — a bare goroutine with **no
   persistence, no retry, and no shutdown drain** (`api/pkg/trigger/trigger.go:32`).
3. If that single HTTP call is lost, nothing retries it. The worker's scheduled jobs are
   `sessions-watcher`, `update-status-reminder`, `deactivate-pending-mentors` and
   `randomize-sort-order` (`api/internal/worker/cron.go:45-53`) — **`new-mentor-watcher` is not among
   them**, so there is no reconciliation loop.
4. `randomize-sort-order` would set `sort_order`, but it selects `WHERE status = 'active'`
   (`api/internal/worker/repository.go:495`), so a stuck `draft` row is never touched.
5. `sort_order` stays NULL forever. Every `ScanMentor` path selects it into a non-pointer `int`
   (`api/internal/models/mentor.go:35,142`), which pgx rejects for NULL.
6. `GetByEmail` therefore errors, and the login endpoint returns its generic response. **The mentor
   cannot log in, is not moderated, and receives no confirmation email** — with no recovery path but
   manual SQL.

**And `P1` is a guaranteed trigger for step 3**: the nil-S3 panic kills the process *after* the row is
committed, so the trigger never completes. `P1` and `P2` are one failure, not two. Fix both together.

### Chain B — a leaked review identifier becomes phishing in a mentor's inbox

1. `request_id` is a bearer capability: `SubmitReview` gates only on Turnstile, the UUID, and DB
   eligibility — no email, no session, no signature (`api/internal/services/review_service.go:124-182`).
   `CheckReview` has no auth at all and returns the mentor's name.
2. It is emitted to PostHog as both a property **and** the `distinct_id`
   (`analytics.RequestDistinctID` → `request:<uuid>`, `api/pkg/analytics/tracker.go:322`), so it
   becomes a queryable *person* record — plus OpenTelemetry `url.path` and every request log line on
   both sides.
3. Anyone holding that UUID can submit a review whose `review_text` (5,000 chars) is interpolated
   **unescaped** into `new-review.json` and emailed to the mentor from your DKIM-signed domain (`P6`).

Fixing either link breaks the chain; fix both (`P14`, `P6`).

---

## 6. Phase 0 — diagnostics (run before changing code)

Read-only. Run these first so you know the blast radius, and so you can repair while you have context.
All three were executed against a dev database and are known to parse.

> **Schema warning.** The `reviews` table has **no** `request_id` or `review_text` column. The real
> columns are `client_request_id`, `mentor_review` and `platform_review`. Queries written from the Go
> models will fail — this bit an earlier draft of this plan.

### D1 — mentors locked out by Chain A

```sql
SELECT id, email, name, status, created_at
FROM mentors
WHERE sort_order IS NULL
ORDER BY created_at;
```

Every row is a person who signed up and silently cannot log in. They will not have complained through
the app, because the login endpoint returns an enumeration-safe success message either way.

**Repair — apply `P2`'s `COALESCE` fix first; for most rows that is the whole repair.** Once it ships,
login, the public profile page and the catalog all work again for every `D1` row, with no data change.

**Then do NOT replay finalization across the list.** `POST /jobs/new-mentor-watcher?mentorId=…`
(`api/internal/worker/jobs.go:110`) is **not idempotent**: `NewMentorWatcher` rewrites `status`
unconditionally, mints a fresh confirmation token — invalidating the link already sitting in the
mentor's inbox — re-randomizes `sort_order` and sends another email. `D1` is a mixed bag: imported
profiles land there with `status='inactive'`, and an `active` row would count *itself* as a duplicate
and be set to `declined`, removing a live mentor from the catalog.

Follow [`docs/runbooks/audit-2026-08/data-repair.md`](../runbooks/audit-2026-08/data-repair.md) §D1,
which is written for this: classify with `D1b` (`diagnostics.sql`), act only on `stuck_registration`
rows, exclude anything created in the last **15 minutes** (its own fire-and-forget finalization may
still be in flight), skip rows that have an active same-email duplicate, and re-check each row
immediately before the call. Consider emailing the affected people; from their side the product was
broken.

### D2 — prices silently overwritten with `Free` (Chain: `P4`)

```sql
SELECT id, email, name, price, created_at, updated_at
FROM mentors
WHERE price = 'Free' AND updated_at > created_at
ORDER BY updated_at DESC;

-- exposure: how many mentors are one save away from losing their price
SELECT count(*) FROM mentors
WHERE price IS NOT NULL
  AND price NOT IN ('Free','$50','$100','$150','$200','Negotiable');

-- the same exposure on `experience`, which has the identical defect (§4.1).
-- Off-list values are rewritten to the first option, '2-5', on the next save.
SELECT COALESCE(experience, '<NULL>') AS experience, count(*) AS mentors
FROM mentors
WHERE experience IS NULL OR experience NOT IN ('2-5', '5-10', '10+')
GROUP BY 1 ORDER BY mentors DESC, 1;
```

On the dev database the second query returned **5 of 14 mentors (36%)**, holding `$20`, `$30`, `$40`.
`diagnostics.sql` runs these as `D2a`–`D2d` with the reasoning attached.

**This is not cleanly recoverable from the database** — the old value was overwritten in place with no
audit trail. Recovery sources, best first: a pre-corruption `pg_dump` from S3 (but see `P8` — backup
failures have been silent, so confirm one restores before relying on it); or recompute for migrated
mentors, since `infra/migration/migrate-mentors.js:393-410` (`mapPrice`) is deterministic.
**Fix `P4` before restoring any values**, or the next save re-corrupts them.

### D3 — has the email injection been exercised?

Synced with `docs/runbooks/audit-2026-08/diagnostics.sql` D3, which is the maintained copy — run that
if you can. Two things this plan originally got wrong (see §4.1): it omitted
`client_requests.preferred_contact` and `mentors.price`, which are unescaped email sinks too, and it
used `\b`, which in PostgreSQL means BACKSPACE — so the tag-name branch matched nothing at all.

```sql
  SELECT id, created_at, 'client_requests.description' AS field
    FROM client_requests
   WHERE description ~* '<\s*(a|img|div|table|script|style)\y'
UNION ALL
  SELECT id, created_at, 'client_requests.name'
    FROM client_requests
   WHERE name ~* '<\s*[a-z]'
UNION ALL
  SELECT id, created_at, 'client_requests.preferred_contact'
    FROM client_requests
   WHERE preferred_contact ~* '<\s*[a-z]'
UNION ALL
  SELECT id, created_at, 'mentors.name'
    FROM mentors
   WHERE name ~* '<\s*[a-z]'
UNION ALL
  SELECT id, created_at, 'mentors.price'
    FROM mentors
   WHERE price ~* '<\s*[a-z]'
UNION ALL
  SELECT id, created_at, 'mentors.calendar_url'
    FROM mentors
   WHERE calendar_url IS NOT NULL
     AND calendar_url <> ''
     AND calendar_url !~* '^https://'
UNION ALL
  SELECT id, created_at, 'reviews.mentor_review'
    FROM reviews
   WHERE mentor_review ~* '<\s*[a-z]'
UNION ALL
  SELECT id, created_at, 'reviews.platform_review'
    FROM reviews
   WHERE platform_review ~* '<\s*[a-z]'
ORDER BY created_at;
```

`preferred_contact` → `{{mentee_contact}}` in `new-request-mentor` and `price` → `{{request_price}}`
in `new-request` / `new-request-calendly` (`job_new_request_watcher.go:99,109-123`). Both are free
text with only a length bound, so a clean `D3` that skipped them proved nothing about the two sinks
most likely to be reached.

Hits are not proof of an attack — but a `javascript:` calendar URL or an `<a href>` inside a name is,
and would turn this into a disclosure question rather than only a code question.

### D4 — outstanding review capabilities (sizes the `H4` decision)

```sql
SELECT count(*) FROM client_requests cr
WHERE cr.status = 'done'
  AND NOT EXISTS (SELECT 1 FROM reviews r WHERE r.client_request_id = cr.id);
```

On dev this returned **138 of 141 requests (98%)**. If production has the same shape, expect to need
a reissue or dual-read path rather than a clean cutover.

---

## 7. Phase 0 — work items P1–P16

Fix in roughly this order; `P1`+`P2` and `P8`+`P9` are natural pairs. Realistic total: **7–10 focused
days**, dominated by `P6` and `P10`.

---

### P1 — A nil S3 client kills the entire API process
**Severity: critical · Effort: S · Area: api**

**Where**
- `api/cmd/api/main.go:271-284` — `storageClient` is only constructed `if AccessKeyID != "" && SecretAccessKey != ""`; otherwise it stays `nil` and is injected anyway.
- `api/config/config.go:371-394` — `Validate()` calls 8 sub-validators; **none** validates S3.
- `api/internal/services/registration_service.go:200` — unconditional `s.storageClient.UploadImageAllSizesAsync(...)`.
- `api/pkg/s3storage/storage.go:240-253` — bare `go func()`, no `recover()`.
- `api/internal/services/profile_service.go:165` — the synchronous sibling path.

**Evidence** — one real `POST /api/v1/register-mentor` with a photo and S3 unset:

```
=== API pid before ===  48505
=== POST /api/v1/register-mentor ===  HTTP 000
=== API pid after ===   >>> PROCESS IS GONE <<<

panic: runtime error: invalid memory address or nil pointer dereference
[signal SIGSEGV: segmentation violation code=0x2 addr=0x8]
  s3storage.(*StorageClient).UploadImage(0x0, ...)             storage.go:97
  s3storage.(*StorageClient).UploadImageAllSizes(0x0, ...)     storage.go:216
  s3storage.(*StorageClient).UploadImageAllSizesAsync.func1()  storage.go:245
created by ...UploadImageAllSizesAsync in goroutine 96          storage.go:244
```

Three further proofs:
- **Gin's recovery middleware cannot catch this.** Tested with the app's real `RecoveryMiddleware` and two routes: an inline panic was recovered (HTTP 500, process alive 2s later); the async path returned **`HTTP 200 {"success":true}` and then killed the process**. `recover()` is per-goroutine.
- **The row commits before the crash** — verified `status=draft, sort_order=NULL`, which is exactly Chain A's precondition.
- **`Validate()` accepts this in production** — with `APP_ENV=production` and empty S3 settings it returned `nil`; controls confirm empty `JWT_SECRET` and `WORKER_AUTH_TOKEN` *are* rejected. **Partial** S3 config (creds set, endpoint/region empty) also returns `nil`.

The three image validators are nil-safe (`nil.ValidateImageSize(...)` returns `nil`), so they do not shield the call. Registrations with *no* photo are safe (`ValidateImageType("")` errors first).

**Why it matters** A production deploy missing `S3_STORAGE_ACCESS_KEY` boots green with no warning, tells the first registrant they succeeded, and then takes the whole API down along with every concurrent request.

**Fix**
1. Add an S3 validator to `config.Validate()` covering empty **and partial** configuration, gated on production like the JWT/worker-token validators. *Or*, if uploads-disabled is a supported mode, make it explicit and reject image-bearing requests **before** any DB mutation.
2. Nil-guard `storageClient` in `RegistrationService` and `ProfileService`, returning a typed "uploads unavailable" error.
3. Add `defer func() { if r := recover(); r != nil { /* log */ } }()` inside the `UploadImageAllSizesAsync` goroutine — or introduce a small `SafeGo` helper and use it for every detached goroutine on a request path. Do this **regardless** of 1 and 2: no background task should be able to terminate the process.

**Acceptance**
- Starting `cmd/api` with `APP_ENV=production` and empty *or* partial S3 config fails at startup with a clear message.
- With a nil storage client, a registration-with-photo returns a non-2xx error and the process stays alive.
- A panic injected into the async upload goroutine is logged and does not terminate the process.

**Regression test** `docs/audit/verification/proofs/nilclient_auditverify_test.go.txt` (asserts the child process survives) and `s3validate_auditverify_test.go.txt` (its `productionBaseline()` helper is a reusable fixture).

**Gotchas** `RecoveryMiddleware` itself nil-panics inside `logger.Error` if the global zap logger was never initialised — irrelevant in production, but it will bite you in tests.

---

### P2 — Mentors are permanently locked out, and told their login link was sent
**Severity: critical · Effort: S · Area: api**

**Where**
- `api/internal/services/registration_service.go:150-170` — inserts `sort_order = nil`.
- `api/internal/repository/mentor_repository.go:115, 379, 513, 547` — **four** `ScanMentor` queries select `sort_order` raw (§4.1: this plan first said three). In order: `fetchMentorByUUIDFromDB`, `GetByEmail` (login), `FetchAllMentorsFromDB` (**the public catalog** — one active row with a NULL breaks the listing for everyone), `FetchSingleMentorFromDB` (the public profile page at `/<slug>`). `GetByLoginToken` (`:404`) scans into `*int` and is not affected.
- `api/internal/models/mentor.go:35, 142` — `SortOrder int`, scanned via `&m.SortOrder`.
- `api/internal/repository/mentor_repository.go:668` — `GetForModerationByID` already does `COALESCE(m.sort_order, 0)`. The fix pattern is in the file.
- `api/internal/services/mentor_auth_service.go:80` — the misleading log line.

**Evidence** — real login request for a mentor left with `sort_order = NULL`:

```
HTTP 200  {"success":true,"message":"If an account exists for that email, a login link has been sent."}

WARN services/mentor_auth_service.go:80  Login request for unknown email
     email=a***@example.com
     error="can't scan into dest[15] (col: sort_order): cannot scan NULL into *int"
```

Causality isolated — same email, same request, only the NULL changed:

```
sort_order = NULL  ->  scan error, no token issued, false-success response
sort_order = 0     ->  no error, TOKEN ISSUED (verified in DB)
```

**Why it matters** Two aggravating factors beyond the lockout:
- **The user is told the link was sent.** They wait for an email that was never generated, retry, and get the same reassurance every time. No error surfaces to them or to any alert.
- **The log actively misleads the operator.** "Login request for unknown email" leads whoever debugs it to conclude the user mistyped their address. The email is not unknown; the row exists and the scan failed.

**Fix**
1. `COALESCE(sort_order, 0)` in **all four** `ScanMentor` queries. (Alternative: give the column a default and backfill. `COALESCE` is preferred — it matches the existing pattern at `:668` and is additive, satisfying §3.)
2. Fix the log line to distinguish "no such row" from "row failed to scan", and log at `error` for the latter.
3. Run `D1`, classify with `D1b`, and repair per `data-repair.md` §D1 — **not** by replaying finalization over the whole list (§4.1).
4. Address the *cause* — the lost finalization — in the same PR or the next: add a `finalize-stuck-registrations` cron job that finds `status='draft'` rows older than N minutes whose finalization never ran and invokes the same handler. The handler is **not** idempotent (§4.1), so the selection is what keeps the job safe: `sort_order IS NULL AND status='draft' AND activated_at IS NULL`, with the same age cutoff, so a row that finalized normally is never touched a second time. ~50 lines, no new dependency; also covers worker downtime and deploy restarts. (See §12 for why a durable queue is deliberately not the Phase 0 answer.)

**Acceptance**
- A mentor row with `sort_order IS NULL` can request a login link and a token is issued.
- `GET /mentor/profile` succeeds for such a mentor.
- A registration whose finalization trigger never fires converges within one cron interval.
- `D1b` returns zero `stuck_registration` rows after repair. **`D1` itself is not expected to reach
  zero** — imported profiles carry a NULL `sort_order` from getmentor.dev and are harmless once the
  `COALESCE` fix ships. Do not chase a zero `D1` by triggering finalization on them (§4.1).

**Regression test** A repository/service test asserting a NULL-`sort_order` mentor is returned by `GetByEmail`; a worker test asserting a dropped trigger still converges.

---

### P3 — Image decompression bomb OOM-kills the API, with no captcha required
**Severity: critical · Effort: S · Area: api**

**Where**
- `api/pkg/imageclass/imageclass.go:63-72` — `image.Decode` is the first action; `borderLuminance` (`:114-151`) then walks ~22% of all pixels.
- `api/pkg/s3storage/storage.go:149-167, 170-184` — `ValidateImageContent` sniffs only the first 512 bytes; `ValidateImageSize` caps *compressed* bytes at 10 MiB.
- Three call sites: `registration_service.go:146`, `profile_service.go:192`, and the admin picture path (`api/cmd/api/main.go:170`).

**Evidence** `[MEASURED]` with crafted 1-bit grayscale PNGs, via `Getrusage` and `/usr/bin/time -l`:

| Bomb | File size | Base64 in body | HeapAlloc | Peak RSS | Wall | Amplification |
|---|---|---|---|---|---|---|
| 10000² | 11.9 KiB | 16 KB | 95.4 MiB | 110 MiB | 0.28 s | 8,185× |
| 20000² | **47.5 KiB** | 65 KB | 381.5 MiB | **397 MiB** | 1.11 s | 8,219× |
| 40000² | **189.9 KiB** | 253 KB | 1,525.8 MiB | **1,543 MiB** | 4.43 s | **8,227×** |

- All four validators returned `nil` for every bomb.
- `GOMEMLIMIT` does **not** help (tested: soft limit, 233 GC signals, still completed at 1,543 MiB) — the allocation is one contiguous live `[]uint8`.
- Backend `mem_limit` is 512m (`infra/docker-compose.yml:263`) → 3× overshoot. The **47.5 KiB** bomb alone reaches 397 MiB, so two concurrent requests suffice.
- The fix is nearly free: `image.DecodeConfig` returned the true 40000×40000 dimensions in **5.6 µs** versus 2.49 s for a full decode.
- `grep -rn DecodeConfig api/` → **0 hits**. No `maxPixels`/`maxWidth` bound either.

**Why it matters — and why this is worse than the source audits said.** Both audits noted that on `/register-mentor` the captcha is checked first (`registration_service.go:89` before `:146`), so each attempt costs a solved Turnstile token. **That is only true for that path.** `POST /api/v1/mentor/profile/picture` reaches the same `classifyPhotoStyle` via `profile_service.go:192` behind **mentor-session auth only, no captcha**, rate-limited at `NewRateLimiter(10, 20)`. Anyone can register a mentor account, so the practical attack is: register once, then repeatedly POST a 47 KB file to your own profile and OOM the API at will. On that path the S3 upload also **succeeds first**, so it burns storage on every shot.

**Fix** Call `image.DecodeConfig` before `image.Decode`; reject on a pixel budget (~40e6 px) plus an aspect-ratio bound. Apply on **all three** photo paths, not just registration. Then decode **once** and pass `[]byte` down (this also fixes `C5`'s repeated decoding). Return a clear 4xx so legitimate large-photo users understand the limit.

**Acceptance**
- A 40000×40000 PNG is rejected by validation, before any full decode, on all three photo endpoints.
- Rejection happens in ~milliseconds and peak RSS stays flat.
- A normal 2000×2000 photo still uploads and classifies correctly.

**Regression test** `docs/audit/verification/proofs/bomb_auditverify_test.go.txt` — flip to asserting rejection, and generate the fixture in-process rather than reading `/tmp`.

---

### P4 — Saving a profile silently destroys the mentor's price
**Severity: high · Effort: S · Area: web**

**Where**
- `web/src/components/forms/ProfileForm.tsx:426` — `<select {...register('price')} defaultValue={mentor.price} ...>`, **uncontrolled**, no placeholder option.
- `web/src/config/filters.ts:82-85` — `price: ['Free','$50','$100','$150','$200','Negotiable']`, commented as "Suggested options for **legacy** price selects".
- `api/migrations/000001_initial_schema.up.sql:20` — `price TEXT`, free text. Go validation is `required,max=100` only.
- `web/src/pages/mentor/profile/edit.tsx:202-215` — forwards the payload verbatim, no field filtering.
- Contrast: `web/src/components/forms/RegisterMentorForm.tsx:722-733` has an `<option value="">` placeholder; `web/src/pages/admin/mentors/[id].tsx:782-783` is **controlled**.

**Evidence** React selects the first non-disabled option when nothing matches, and react-hook-form reads that DOM value at mount:

```
stored "$75"        -> select.value = "Free"
stored "$30 / hour" -> select.value = "Free"
submitted payload price = "Free"   <-- mentor only edited their job title
control: "$100"     -> round-trips correctly as "$100"
admin form, "$75"   -> payload price = "$75"   (controlled: preserved)
registration form   -> starts at "", submit blocked
```

Exposure `[MEASURED]` on dev: **5 of 14 mentors (36%)** hold out-of-list prices (`$20`, `$30`, `$40`). `mapPrice` (`infra/migration/migrate-mentors.js:393-410`) rounds RUB→USD to $5 increments (`$5, $10, ... $75, $125`), of which only four values exist in the list, and its raw pass-through (`if (/^\$?\d+/.test(raw)) return raw`) emits things like `$30 / hour` and `75 USD`.

**Why it matters** Silent, unrecoverable, revenue-relevant data loss triggered by the victim's own unrelated edit. The value also flows into the mentee confirmation email (`api/internal/worker/job_new_request_watcher.go:99`) and re-buckets the profile in catalog filters.

**`experience` has the same bug — fix both fields in this change.** §4.1: this plan first claimed
`experience` was unaffected. It is rendered identically (`ProfileForm.tsx:404-409`, uncontrolled
`{...register('experience')}` + `defaultValue`, no placeholder) against the three options `2-5`,
`5-10`, `10+`, and the column is free `TEXT` with no `CHECK`. The importer's `mapExperience`
(`migrate-mentors.js:412-417`) passes unknown source values through verbatim, so imported profiles are
the likely holders — and their next save silently rewrites the value to `2-5`. `diagnostics.sql` **D2d**
counts the exposed rows, and `data-repair.md` §D2 covers it with the same recovery sources as `price`.

**Fix** Preserve unknown values, in **both** selects. Simplest: render the stored value as an extra `<option>` when it is not in the list. Cleaner long-term: make the field a free-text input or `<datalist>`, matching the fact that the column is free text. Either is acceptable — pick one and note it.

**Acceptance**
- Rendering `ProfileForm` with `price: '$75'` shows `$75`, and saving an unrelated field submits `price: '$75'`.
- `price: '$100'` still round-trips.
- Rendering with `experience: 'lots'` shows `lots`, and saving an unrelated field submits `experience: 'lots'`; `experience: '5-10'` still round-trips.

**Regression test** A `ProfileForm` test parameterised over `['$75','$30 / hour','$5','$125']` asserting round-trip, plus the `$100` control — and the same shape for `experience` over an off-list value plus an on-list control.

**Gotchas** When testing the admin page, hoist mocked `useAdminAuth`/`useRouter` return values to module-level constants — returning a fresh object per render causes an infinite render loop and a 15s timeout.

---

### P5 — Shell injection in the deploy workflow gives code-exec on the runner and the VM
**Severity: critical · Effort: S · Area: ci**

**Where** `.github/workflows/deploy.yml:244` (the "Set image tags on VM" step); the dispatch input is declared at `:176-182`.

**Evidence** The step immediately above (`:170-184`) correctly routes the input through `env:` — its comment even cites the pattern — and then `:244` abandons it:

```yaml
ssh -i ~/.ssh/deploy_key -p 22 "${{ secrets.VM_SSH_USER }}@${{ secrets.VM_SSH_HOST }}" \
  "bash -s" -- "${{ github.event.inputs.service }}" "${{ steps.tag.outputs.TAG }}" <<'TAG_SCRIPT'
```

`steps.tag.outputs.TAG` is the free-text `image_tag` input verbatim. Reproducing the construction locally:

```
bash -c 'echo SERVICE=$1 TAG=$2' -- both "x"; touch /tmp/PWNED; echo ""
>>> INJECTION SUCCEEDED (file created)
```

**Why it matters** Any collaborator with write access (enough to run `workflow_dispatch`) gets arbitrary code execution on the runner — which holds the OIDC AWS role and `VM_SSH_KEY` — and on the production VM, bypassing code review entirely. `service` is safe because it is a `choice` input.

**Fix** Pass both values via `env:` and reference `"$TAG"` / `"$SERVICE"` inside the `run:` block, matching `:170-184`. Add an input format check (`^[A-Za-z0-9._-]+$`) and fail fast. Audit the rest of the file for the same pattern — `:298` already uses `printf '%q'`, so the codebase knows the technique.

**Acceptance** A dispatch with `image_tag` set to `x"; touch /tmp/pwned; echo "` fails validation (or is passed through inertly) and executes nothing. No `${{ ... }}` interpolation of user-controllable input remains inside any `run:` shell line in the file.

**Regression test** Not unit-testable. Add `actionlint` to the infrastructure CI gate (`H12`) — it flags this class of injection.

---

### P6 — User input is interpolated unescaped into SES email templates
**Severity: high · Effort: S–M · Area: api**

**Where**
- `api/pkg/email/sender.go:100-133` — `json.Marshal(props)` straight into SES `TemplateData`; templates are sent **inline** via `Template.TemplateContent{Subject, Html, Text}`.
- `api/pkg/email/templates/templates.go` — unmarshals the asset and hands the parts to SES; no escaping.
- Props assembled in `api/internal/worker/job_new_request_watcher.go:92-127`, `job_process_review.go:84-91`, `job_mentor_moderation.go:166-176`, `job_mentor_confirmed.go:60`.
- The only two `html.EscapeString` call sites in the whole backend: `job_request_finished.go:147,152` and `job_cron.go:60-63`.

**Evidence** With the payload in `mentee_request`, `TemplateData` carries it byte-for-byte (the `<` in the JSON is `encoding/json`'s transport escaping, which SES decodes back to `<`), and the rendered HTML body contains `<a href="https://evil.example/pay">Pay now</a>` as **live markup**. Reproduced for `request_details` (`new-request`) and `review_text` (`new-review`).

Fields confirmed in an HTML-text context, unescaped:

| Prop | Source | Sink | Cap |
|---|---|---|---|
| `request_details` / `mentee_request` | contact `Intro` (**unauthenticated**) | `new-request*.json` body | 4000 |
| `mentee_contact` | `PreferredContact` | `new-request-mentor.json` | 100 |
| `first_name` / `mentee_name` | contact `Name` | 4 templates | 100 |
| `mentor_name` | mentor profile | `new-request.json` ×4 **and a Subject line** | — |
| `review_text` | review body | `new-review.json` | 5000 |
| `reviewer_note` | moderator note | `new-mentor-returned.json` | 2000 |
| `calendly_url` | `CalendarURL` | `href="{{calendly_url}}"` | 500 |

A working payload inside the 100-char `contact` cap (55 chars): `x</div><a href="https://evil.example/pay">Pay now</a><div>`.

The `url` binding does not save you: `validator/v10` `baked_in.go:1530-1556` requires only a scheme plus host/opaque — it does **not** restrict to http(s) (`isHttpURL` is the separate, unused `http_url` tag) — and Go's `url.Parse` accepts `"` and `>` in the path. So `https://evil.example/x">` and `javascript:alert(1)` both pass validation.

Note the codebase's own two `html.EscapeString` sites exist precisely because those two jobs hand-assemble HTML — proof the authors knew the sink renders raw markup.

**Why it matters** An unauthenticated contact submission injects a styled phishing panel into email sent from your DKIM-signed domain to your mentors. Via Chain B, anyone holding a `request_id` can do the same through `review_text`. **Calibration: this is HTML injection → phishing/brand damage, not stored XSS in the app and not code execution** — mail clients strip `<script>`.

**Fix — render locally; do not escape props.** The obvious cheaper fix is to HTML-escape props before handing them to SES. That was tested and **does not work**, because SES uses one `TemplateData` blob for the Subject, the HTML part, and the plaintext part:

- **19 of 19 templates have a non-empty `text` part**, interpolating the same variables as the HTML part.
- **6 templates interpolate a user-controlled name into the Subject** (`{{mentor_name}}` in `new-request`, `new-request-calendly`, `new-request-moderator`, `session-declined`, `new-mentor-moderator`; `{{mentee_name}}` in `new-request-mentor`).
- Only **3** templates have any HTML-only variable (`requests_list`, `decline_info`).

Rendering with escaped props produces, verbatim:

```
SUBJECT: Your request is on its way to Tom &amp;amp; Jerry
TEXT:    Hi O&amp;#39;Brien,
         Message: I want to learn C++ &amp;amp; Go (rates &amp;lt; $50)
```

Entity noise in the Subject header (never HTML) and in every plaintext body. The only correct variant of escape-at-the-boundary is **paired props** (`x` / `x_text`) — which the codebase already uses for `decline_info`/`decline_info_text` and `requests_list`/`requests_list_text` — but extending that to ~8 fields means duplicating props across 19 assets and remembering it for every future prop, which is exactly the failure mode that created this bug.

Local rendering is also cheap, because there is nothing to reimplement: templates are **already shipped inline** on every API call, and there are **no** Handlebars block helpers (`{{#if}}`, `{{#each}}`, `{{^}}`) and **no** triple-brace `{{{raw}}}` anywhere — pure `{{var}}` substitution. Steps:

1. At template load, rewrite `{{name}}` → `{{.name}}` with one regex. Go templates index a `map[string]any` fine. **No asset edits needed.**
2. Parse the `html` part with `html/template`; parse `subject` and `text` with `text/template`. Use `missingkey=error`. This is the point: the HTML part gets *context-aware* escaping while subject and plaintext get none — correct in all three contexts at once, which one shared blob cannot achieve.
3. Send as SES `Simple` content (`Subject`, `Body.Html`, `Body.Text`) instead of `Template` + `TemplateData`.
4. **Rebuild** the two intentional server-generated fragments (`job_request_finished.go`, `job_cron.go`) as `html/template` templates and return the result as `template.HTML`, so the fragment's own markup survives while the values inside it are escaped structurally. **Do not simply wrap today's concatenated string in `template.HTML` and delete the `html.EscapeString` calls** — `html/template` does not inspect a `template.HTML` value, so that hands the outer template pre-rendered raw markup and re-creates the injection through `DeclineComment` and `reviewer_note`. §4.1: that is what this step originally said. A parity test on the escaped inner value (`Try again <soon>` must render as `Try again &lt;soon&gt;` in the HTML part and verbatim in the `_text` part) is what tells the two constructions apart.
5. Independently, validate calendar URLs as https-with-host at ingest (`api/internal/models/profile.go:15`, `mentor_registration.go:24`, `admin_moderation.go:157`).

Roughly 40 lines plus tests. `html/template` additionally neutralises dangerous schemes in `href` for free, case-insensitively, while preserving benign URLs:

```
javascript:alert(1)                       -> <a href="#ZgotmplZ">
JaVaScRiPt:alert(1)                       -> <a href="#ZgotmplZ">
data:text/html,<script>alert(1)</script>  -> <a href="#ZgotmplZ">
https://ok.example/x?a=1&b=2              -> <a href="https://ok.example/x?a=1&amp;b=2">
```

Per-prop escaping cannot do that — escaping a scheme changes nothing.

**Trade-offs to accept knowingly:** you lose SES-side template management (irrelevant today, since templates are already inline — but note `SendBulkEmail` requires *stored* templates, so a future bulk-send feature would need them registered); and template changes require a deploy, which they effectively already do.

**Acceptance**
- No user-controlled prop can emit raw `<` into the rendered HTML part.
- Subject lines and plaintext bodies contain **no** HTML entities for inputs like `Tom & Jerry`, `O'Brien`, `5 < 10`.
- `javascript:` and `data:` URLs in `calendly_url` do not produce a working link; a valid https URL still does.
- The two server-generated fragments still render as markup.
- Every template parses and renders against a fixture.

**Regression test** `docs/audit/verification/proofs/injection_auditverify_test.go.txt` — five sub-tests covering all of the above.

---

### P7 — Contact flow: upstream 4xx becomes 500, and the captcha token is never reset
**Severity: high · Effort: S · Area: web**

**Where**
- `web/src/pages/api/contact-mentor.ts:24-29` — bare `catch` → unconditional 500. Same shape in `schedule-migration.ts`, `reviews/submit.ts`, `reviews/check.ts`.
- `web/src/lib/api-proxy.ts` — `sendUpstreamError`, already used by **24** routes.
- `web/src/components/forms/ContactMentorForm.tsx:211-216` — renders `<Turnstile>` with **no `ref`**.
- Contrast `web/src/components/forms/RegisterMentorForm.tsx:197, 205-210` — holds `turnstileRef` and runs `useEffect(() => { if (isError) { turnstileRef.current?.reset(); setCaptchaToken('') } }, [isError])`.

**Evidence**

```
contact-mentor   upstream 400 -> 500 {"error":"Internal server error"}
contact-mentor   upstream 429 -> 500 {"error":"Internal server error"}
register-mentor  upstream 400 -> 400 {"error":"username already taken"}   (correct)
register-mentor  upstream 503 -> 500                                      (correct: hide 5xx)

ContactMentorForm   reset() calls = 0    tokens: first="token-1" second="token-1"  <-- spent token resent
RegisterMentorForm  reset() calls = 1    tokens: first="token-1" second="token-2"  <-- fresh
```

**Why it matters** This is the platform's primary conversion path. A Turnstile rejection or validation error reaches the user as "Something went wrong", and every retry resubmits a spent single-use token that `siteverify` rejects. The form stays mounted (`web/src/pages/mentor/[slug]/contact.tsx:299`), so the user sees a live retry button that cannot succeed.

Two calibrations: the loop **self-heals after ~5 minutes** (Turnstile's default `refreshExpired: 'auto'` fires `onExpire`, clearing the token), so "requires a page reload" is inexact — but every retry in a realistic window fails. And the **8 auth routes are not broken**: they hand-roll equivalent 4xx logic inline because they need a `{success, message}` envelope instead of `{error}` (that is duplication for `M2`, not a bug).

**Fix** Replace the four bare catches with `sendUpstreamError(res, error, { context: '...', method: req.method, url: req.url })`. Add `turnstileRef` plus the `isError`-keyed reset effect to `ContactMentorForm`, clearing via `setValue('captchaToken', '')`. `contact.tsx` sets `readyStatus='loading'` at submit, so `isError` toggles `true→false→true` and the effect re-fires on every failure.

**Acceptance**
- An upstream 400/429 reaches the client with that status and the upstream message; an upstream 5xx still becomes a generic 500.
- After a failed submit, the Turnstile widget is reset and the next submit carries a different token.

**Regression test** Route tests with `node-mocks-http` (follow the existing pattern in `web/src/__tests__/pages/api/`), plus a component test observing `reset()`. When mocking `@marsidev/react-turnstile`, issue the new token **asynchronously** (`setTimeout(..., 0)`) inside `reset()` — a synchronous `onSuccess` is clobbered by the `setCaptchaToken('')` that follows it, producing a false failure.

---

### P8 — Backup failures are completely silent
**Severity: high · Effort: S · Area: infra**

**Where**
- `infra/postgres-backup/backup.sh:127-139` — `run_backup` correctly returns 1 on `pg_dump` or S3 failure.
- `infra/postgres-backup/backup.sh:167` — `run_backup || true`, inside the `while true` daemon loop.
- `infra/postgres-backup/backup.sh:137, 145` — on S3 failure the dump is kept locally, but `prune_local` is only called in the no-bucket branch.
- `infra/docker-compose.yml:196-230` — **no `healthcheck:`** for `postgres-backup`.
- `infra/deploy-remote.sh:172-178`, `infra/rollback.sh:209-212` — the only liveness signal is `docker inspect -f '{{.State.Status}}' == running`.
- `infra/alloy/config.alloy:184, 190, 196` — exactly three `loki.source.file` blocks (frontend/backend/worker); none covers this sidecar, and there is no `loki.source.docker`.

**Evidence**

```
$ backup.sh once   (unreachable database)
pg_dump: error: could not translate host name "unreachable.invalid" to address
[postgres-backup] FAILURE db=openmentor file=... error=pg_dump_failed
RUN_BACKUP_EXIT=1

grep -c 'last_success\|touch ' backup.sh                 -> 0   (no freshness marker)
compose healthcheck for postgres-backup                  -> 0
grep -ri backup grafana/                                 -> 0   (no alerting)
```

**Why it matters** `pg_dump` or the S3 upload can fail every night indefinitely with **zero signal**: the exit code is discarded, the container stays `running` so the deploy-time probe passes, there is no healthcheck, no success marker, no alert, and the `FAILURE` line goes to stdout which Alloy never collects. The runbook claims `RPO ≤ 24h` (`docs/runbooks/postgres-backup-restore.md:11,102`), and there is now real user data behind it. `D2`'s price recovery may depend on a historical dump — so whether these backups have *ever* succeeded is an open question, not a hypothetical. Sustained upload failure also accumulates one full-size dump per night on the same disk as the live database.

**Fix**
1. On success, `touch /backups/.last_success`.
2. Add a compose `healthcheck` that fails when that file is older than ~26h.
3. Call `prune_local` on both branches.
4. Make the failure visible: either add a `loki.source.docker` scrape for this container, or write a textfile metric Alloy already collects, and add one Grafana alert on backup staleness.
5. Keep `|| true` in the loop (you do want the daemon to survive a transient failure) — the fix is the marker and the healthcheck, not crashing the sidecar.

**Acceptance**
- A forced `pg_dump` failure leaves `.last_success` stale, turns the container unhealthy within the healthcheck window, and fires an alert.
- A successful run refreshes the marker and the container reports healthy.
- Repeated S3 failures do not grow local disk without bound.

**Verify a restore actually works** while you are here — see `H11`.

---

### P9 — The production env template makes the backup sidecar restart-loop, which rolls back healthy deploys
**Severity: high · Effort: XS · Area: infra**

**Where**
- `infra/.env.production.example:131` — `BACKUP_S3_BUCKET=openmentor-db-backups` (set).
- `infra/.env.production.example:135-137` — comment promising a fallback to `S3_STORAGE_ACCESS_KEY`/`SECRET_KEY`.
- `infra/.env.production.example:138-139` — `BACKUP_AWS_ACCESS_KEY_ID=` / `BACKUP_AWS_SECRET_ACCESS_KEY=` (both empty).
- `infra/postgres-backup/backup.sh:44-59` — the fallback was **deleted**; it now `exit 1`s.
- `infra/.env.example:97-105` — already carries the corrected wording. Only the production template was missed.

**Evidence** Run with the template's exact defaults, *and* with app S3 keys populated to test the promised fallback:

```
[postgres-backup] FATAL: BACKUP_S3_BUCKET is set but BACKUP_AWS_ACCESS_KEY_ID / ... are not.
[postgres-backup] Provide dedicated backup credentials (no fallback to app S3 keys). Refusing to start.
EXIT=1
```

The app keys were present and ignored — **the documented fallback provably does not exist**.

**Why it matters** `exit 1` under `restart: always` is an endless loop. The container's status is then `restarting`, not `running` — which `infra/deploy-remote.sh:173` reads as unhealthy, setting `HEALTH_CHECK_FAILED=1` and triggering an **automatic rollback of an otherwise perfectly healthy application deploy** (`:181-211`). `rollback.sh:209-212` fails the same way. A first-time production bring-up following `infra/deploy.sh:146`'s own instruction (`cp .env.production.example .env.production`) cannot complete a deploy. This also violates the env-contract rule in `CLAUDE.md`.

**Fix** Mirror `infra/.env.example:97-105`'s wording into `.env.production.example:135-137`, and either leave `BACKUP_S3_BUCKET` empty in the template or fill the credential placeholders so the two are self-consistent. Fix the same stale claim in `infra/postgres-backup/backup.sh:18-21` and `docs/runbooks/postgres-backup-restore.md:119`.

**Acceptance** A fresh `cp .env.production.example .env.production`, filled in per its own comments, brings the stack up with the backup sidecar `running` and does not trigger a rollback.

---

### P10 — Every container receives every production secret
**Severity: high · Effort: M · Area: infra**

**Where**
- `infra/docker-compose.yml:94, 218, 243, 272, 277, 310, 372` — `env_file: .env.runtime` on frontend, backend, worker, migrate, postgres-backup and alloy.
- `infra/deploy-remote.sh:65` — `.env.runtime` is the full production env minus exactly two image-tag lines: `grep -vE '^(FRONTEND_IMAGE_TAG|BACKEND_IMAGE_TAG)=' .env > .env.runtime`.
- `infra/docker-compose.yml:388-391` — one flat bridge network; the comment even notes it is deliberately not `internal`.

**Evidence** Compose expansion yields 101–103 environment keys per affected service. The internet-facing frontend therefore holds `DATABASE_URL`, `POSTGRES_PASSWORD`, `JWT_SECRET`, `WORKER_AUTH_TOKEN`, SES/S3/backup keys, `CLOUDFLARE_DNS_API_TOKEN` (domain takeover) and `GCLOUD_RW_API_KEY` — **and** shares a network with Postgres, which publishes no host port but is directly reachable in-network. Neither Cloudflare nor Grafana credentials are used by the Go API at all; they ride along only because of the shared file.

**Why it matters** A frontend RCE or a dependency compromise is one hop from database takeover, credential exfiltration, worker-callback forgery, email abuse, DNS modification and observability access. File permissions do not help — the values are deliberately injected into the process environment.

**Fix, strictly in this order** (rotating first just redistributes new secrets to the same six containers):

1. Replace shared `env_file` with explicit per-service `environment:` allowlists. Inventory what each binary actually reads: `rg 'Getenv|LookupEnv|process\.env' api web infra`. Record the service→key matrix in `infra/ENVIRONMENT_VARIABLES.md`.
2. Keep deploy/build-only values (`VM_*`, Cloudflare, sourcemap upload credentials) out of runtime containers entirely.
3. Add a CI check that renders Compose to JSON and diffs each service's env **key names** — never values — against a committed allowlist. Wire it into the infrastructure gate (`H12`).
4. Deploy and verify.
5. **Then rotate**, in this order: database passwords → S3/SES/backup keys → `WORKER_AUTH_TOKEN` and internal API tokens → Cloudflare DNS token → `GCLOUD_RW_API_KEY` → `JWT_SECRET` **last**.

**Two rotation side-effects to schedule deliberately:**
- Rotating `JWT_SECRET` **invalidates every live session** — all mentors and admins are logged out and must request a new magic link. Do it in a low-traffic window, and pair it with `P14`'s token invalidation so the disruption is absorbed once.
- Rotating the database password needs a coordinated restart of api/worker/migrate. Confirm `P15` and `P9` are already fixed first, or a deliberate maintenance restart will auto-roll-back.

**Acceptance** From inside the running frontend container, `env` contains **none** of: `DATABASE_URL`, `POSTGRES_PASSWORD`, `JWT_SECRET`, `WORKER_AUTH_TOKEN`, SES/S3/backup credentials, `CLOUDFLARE_DNS_API_TOKEN`, `GCLOUD_RW_API_KEY`. The CI allowlist check fails if a forbidden key is added to a service. All credentials rotated after the new contract is live.

**Judgement call** Per-service `environment:` blocks are more verbose but explicit and diffable; per-service env files are terser but hide the contract. The allowlist CI check matters more than which of the two you choose.

---

### P11 — Confirmation resends are limited to 2 per 5 minutes platform-wide
**Severity: high · Effort: S · Area: api**

**Where**
- `api/cmd/api/main.go:64` — `group.POST("/mentors/confirm/resend", confirmResendRateLimiter.Middleware(), ...)` — the **IP-keyed** middleware.
- `api/cmd/api/main.go` — the limiter is `NewRateLimiter(0.00667, 2)`, i.e. 2 tokens per 5 minutes.
- `api/cmd/api/main.go:101-105` — the login limiters already use `EmailRateLimitMiddleware` for exactly this reason, and the comments there explain why.
- `api/internal/middleware/rate_limit.go:106` — `c.ClientIP()` keying.

**Evidence** Four requests with four **different** email addresses from one source IP:

```
request 1 (unique email) -> HTTP 400
request 2 (unique email) -> HTTP 400
request 3 (unique email) -> HTTP 429
request 4 (unique email) -> HTTP 429
```

Two findings: distinct users share one bucket (confirming IP keying — and in production all traffic arrives from the BFF's single IP, so that bucket is the whole platform); and the payloads were deliberately **malformed** (the endpoint wants `token`, not `email`) yet still consumed budget, proving **the limiter runs before validation**.

**Why it matters** On launch day most users clicking "resend confirmation" get a 429. Worse, any buggy client, retry loop or scanner sending garbage can exhaust the global budget without ever making a valid request.

**Fix** Re-key on the confirmation token via the existing `EmailRateLimitMiddleware` pattern (the token identifies the mentor), and move the limiter **after** binding so malformed payloads do not consume budget. Review `contactRateLimiter` (5/s shared across contact, reviews and migration intents) the same way — either key it or document it as a deliberate global cap with a comment explaining the BFF-IP constraint.

**Acceptance** Two resends for *different* tokens do not compete for the same bucket. Repeated malformed requests do not exhaust the limit for valid ones. A single token still cannot be resent more than the intended rate.

**Regression test** A middleware test asserting per-token isolation, mirroring the existing `middleware/rate_limit` tests.

---

### P12 — The support runbook instructs operators to email nearly every secret
**Severity: high · Effort: XS · Area: docs**

**Where** `infra/docs/troubleshooting.md:839` (inside the "Information to Gather" bundle, `:827-850`), which ends at `:854` with "Send debug-info.txt to support". Same pattern at `infra/docs/troubleshooting.md:490` and `infra/ENVIRONMENT_VARIABLES.md:146`.

**Evidence** The command is `docker exec -it openmentor-backend env | grep -v SECRET >> debug-info.txt`. The denylist filters five keys by name; everything else survives, including `DATABASE_URL` (full DSN with password), `POSTGRES_PASSWORD`, `POSTGRES_OBS_DSN`, `CLOUDFLARE_DNS_API_TOKEN`, `GCLOUD_RW_API_KEY`, `WORKER_AUTH_TOKEN`, `GO_API_INTERNAL_TOKEN`, `METRICS_AUTH_TOKEN`, `POSTHOG_PERSONAL_API_KEY`, and three access-key IDs. The Cloudflare and Grafana tokens are only present because of `P10`.

**Why it matters** This is the procedure people follow under pressure, and the output is destined for email or an issue tracker. A denylist is the wrong shape for this job.

**Fix** Replace with an allowlist plus presence-only checks, e.g. `env | grep -E '^(APP_ENV|LOG_LEVEL|PORT|GIN_MODE|NODE_ENV)='` and, for anything sensitive, print only whether it is set. Fix all three locations.

**Acceptance** Following the runbook verbatim produces a bundle containing no credential values. A reviewer can confirm by diffing the bundle against the env key list.

---

### P13 — Unauthenticated requests grow metric cardinality without bound
**Severity: medium · Effort: XS · Area: web**

**Where**
- `web/src/lib/with-observability.ts:24-42` — `normalizeRoute` collapses only UUID-shaped segments and one `/mentor/<slug>` pattern.
- `web/src/lib/with-observability.ts:48-76` — `activeRequests.inc()` runs **before** the handler does any authentication.
- `web/src/lib/metrics.ts:87-104` — the metrics are `http_server_request_duration_seconds`, `http_server_request_total`, `http_server_active_requests`.

**Evidence** `[MEASURED]` through the real wrapper and the real prom-client registry:

```
distinct http_route series after 500 unique non-UUID ids: 500
normalizeRoute UUID control: /api/mentor/requests/:id     (500 UUIDs -> 1 label)
```

1:1 growth — one new series per unique path value, all remote-written to Grafana Cloud. The UUID control collapsing to a single label confirms the function works *only* for UUID-shaped segments.

**Why it matters** In-process memory growth plus remote series growth, driven by unauthenticated traffic against any dynamic API route. Cheap to trigger, slow to notice.

**Fix (pragmatic, differs from the external audit's proposal)** Add a ~10-line allowlist: if `normalizeRoute`'s output is not in a known set of route templates, label it `other`. That caps cardinality immediately without touching 37 route files. Then migrate to explicit compile-time labels — `withObservability('/api/mentor/requests/:id', handler)` — incrementally in `C7`, rejecting an empty or user-derived label in development.

**Acceptance** Thousands of requests with unique non-UUID path values produce a fixed number of series. Real routes still get distinct, readable labels.

**Regression test** The measurement harness in `verification/README.md` §9, flipped to assert a bounded series count.

**Gotchas** The metric is `http_server_request_total`, not `http_requests_total`. Mock requests need `headers: {}` and `socket: {}` because `web/src/lib/logger.ts:178` reads `req.headers['user-agent']`.

---

### P14 — A review capability and login tokens leak into telemetry
**Severity: high (containment) · Effort: S · Area: web + api**

**Where**
- `api/internal/services/review_service.go:124-182` — `SubmitReview` gates only on Turnstile + the UUID + DB eligibility. `CheckReview` (`:60-121`) has no auth at all and returns the mentor's name. Routes at `api/cmd/api/main.go:66-68`, commented "public - uses captcha for protection".
- `api/pkg/analytics/tracker.go:322` — `RequestDistinctID` → `request:<uuid>`, used as the PostHog **`distinct_id`** in `review_service.go` and `contact_service.go:113,136-138`.
- `api/internal/middleware/observability.go:51-87` — logs the actual path on every request, plus raw route params on 4xx/5xx. It already has a `sensitiveQueryParams` mechanism at `:15-18` — extend it to path segments.
- `web/src/lib/tracing-server.ts:59-74` — no `ignoreIncomingRequestHook`, no attribute sanitiser. `@opentelemetry/instrumentation-http` records `url.query` unconditionally, so `GET /mentor/auth/callback?token=<jwt>` produces a span attribute containing the token.
- `web/src/lib/tracing-server.ts:60-68` — the dead `headersToSpanAttributes` block (see §4: it captures nothing today, but the option name is identical under undici, so a future copy-paste creates a real leak).
- `web/src/pages/mentor/[slug]/contact.tsx:271` — sends `request_id` to analytics; `web/src/lib/analytics.ts:76-93` does not block that key.

**Evidence** `request_id` is a bearer capability — knowing the UUID is sufficient to submit a review attributed to that mentee/mentor pair, and to read the mentor's name. It reaches OpenTelemetry (`url.path` server-side via otelgin, `url.full`/`url.query` on undici client spans), winston/zap on **every** request, and PostHog as both a property and a person identifier. Magic-link tokens leak via OpenTelemetry `url.query` only — the log and PostHog paths are already mitigated (see §4).

**Why it matters** Anyone with trace, log or PostHog read access can submit a fraudulent review, and via `P6` inject phishing HTML into a mentor's inbox (Chain B). Because the system is live, **these values are already in Grafana Cloud and PostHog** and retained for whatever the retention window is.

**Fix — containment now (this is the P0 part)**
1. Add `request_id`/`requestId` to `analytics.ts` `blockedPropertyKeys`; stop passing `RequestDistinctID` for review and contact events (use an anonymous or mentor-scoped ID).
2. Redact `request_id` from Go request/error logging and from `review_service.go`'s zap fields; extend the existing `sensitiveQueryParams` mechanism to cover capability-bearing path segments.
3. Add an OpenTelemetry span-processor filter that scrubs `url.query`/`url.path`/`url.full` for known-sensitive keys and the review route, plus `ignoreIncomingRequestHook` for the auth callback paths. The same filter covers the magic-link `token` leak.
4. Delete the dead `headersToSpanAttributes` block.

**Then, operationally** (this is incident response, because traffic is live):
5. Invalidate all live magic-link and confirmation tokens: `UPDATE mentors SET login_token = NULL, login_token_expires_at = NULL WHERE login_token IS NOT NULL`, and the equivalent for moderators and confirmation tokens. Cost to users is minimal (request a new link); pair with `P10`'s `JWT_SECRET` rotation to absorb the disruption once.
6. Purge or shorten retention on affected Tempo/Loki data and PostHog events where the tooling allows. **PostHog is the sharpest problem**: `request:<uuid>` was used as a `distinct_id`, so those are *person* records — deleting them is a different operation from dropping an event property. Check what person-deletion your plan supports (§11).

**The authorization redesign is separate** — see `H4`. Containment does not depend on it.

**Acceptance** Sentinel-value tests: feed a known secret through the auth-callback and review paths and assert it appears in **no** span attribute, log message, analytics property, or `distinct_id` — including nested, percent-encoded, camelCase and snake_case forms. Live tokens invalidated.

---

### P15 — A broken edge reports a successful deploy
**Severity: medium · Effort: XS · Area: ci**

**Where**
- `.github/workflows/deploy.yml:301-321` — every branch is an `echo`; `curl ... || echo "000"` even swallows curl's exit code; a non-200 prints "might be expected if DNS/SSL is not yet configured"; unset `DOMAIN` skips silently. The step always exits 0, so `:334-340` prints "Deployment completed successfully!" and `notify-failure` (`:351-355`, `if: failure()`) never fires.
- `infra/deploy.sh:650-667` — identical logic, followed unconditionally by "Deployment completed successfully!" at `:671`.
- `.github/workflows/deploy.yml:162` — `if: always() && (needs.build-frontend.result == 'success' || needs.build-backend.result == 'success')`.

**Evidence** The checks that *do* gate (`infra/deploy-remote.sh:136-178`, which auto-roll-back) are all `docker exec <container> curl http://localhost:...` — **loopback inside the container**. Nothing ever traverses Traefik. So Let's Encrypt DNS-01 failure, the duplicate-router collision the compose file documents at `:18-25`, a broken `sec-headers`/`edge-ratelimit` middleware, the www-redirect regex, or DNS can all produce a hard site outage that both deploy paths report as success.

Separately, `always() && (A || B)` lets a `service=both` deploy proceed when the backend build **failed**: `:265-267` writes a tag that was never pushed. It fails loudly at `docker compose pull` (`set -e`), but leaves `.env` pointing at a nonexistent tag — which the next deploy's `cp .env .env.backup` then enshrines as the rollback target.

**Fix** Make the public probe mandatory: require `DOMAIN`, retry with bounds, and `exit 1` on failure. Change the gate to `!cancelled() && needs.build-frontend.result != 'failure' && needs.build-backend.result != 'failure'`. (`always()` is genuinely needed because single-service deploys leave the other build `skipped` — the defect is the `||`, not `always()`.)

**Acceptance** A deploy against a domain whose TLS or DNS is broken fails the workflow and fires `notify-failure`. A deploy where one of two builds failed does not proceed to tag-setting.

---

### P16 — Launch hygiene and security-driven dependency patches
**Severity: mixed · Effort: XS–S · Area: repo + deps**

Small, independent items; batch them into one or two PRs.

**Repo hygiene**
- **`CODEOWNERS` is invalid.** It contains only `@glamcoder` — no path pattern, no trailing newline — so it assigns **no owners** and any "require code-owner review" rule silently matches nothing. Fix: `* @glamcoder`.
- **`web/.gitignore:47` is mangled.** `.ideaweb/public/sample-images/` is two entries concatenated, so `web/.idea/` is tracked, including the fork leftover `getmentor.dev.iml`. Split the line; `git rm -r --cached web/.idea`. Add `yarn-error.log` while there (the project is npm-only).
- **`infra/.env.runtime` (~16 KB of real secrets) sits in the working tree.** Correctly gitignored, so nothing leaked, but `CLAUDE.md` says to delete it after `docker compose config` validation. Delete it.
- **Ko-fi placeholder.** `web/src/config/donates.ts:3-5` is marked TODO and points at `ko-fi.com/openmentor`; it is promoted on the homepage (`web/src/pages/index.tsx:341-348`) and in `web/public/llms.txt`, and the `$3/$10/$25` picker only changes the button label. Confirm the account, then either wire the selected amount into the URL (Ko-fi supports amount params) or remove the picker. Align `.github/FUNDING.yml`.
- Delete `infra/package-lock.json` — an empty stub (`"packages": {}`) with no sibling `package.json`.
- Add `"license": "AGPL-3.0-only"` to `web/package.json` to match the repo LICENSE.

**Security-driven dependency updates** (verified with `govulncheck` and `npm audit`)
- `cd api && go get google.golang.org/grpc@v1.83.0 && go mod tidy` — fixes GO-2026-6061. Indirect dep, low real exposure (no gRPC server), but a one-line fix. Run `make ci`.
- `web/Dockerfile` — `node:26.5.0-alpine3.23` → `node:26.5.1-alpine3.23` (Node security release), all three stages.
- `api/Dockerfile` — pin `golang:1.26-alpine` → `golang:1.26.5-alpine`. A floating tag plus Docker layer cache can silently retain an old digest carrying 19 reachable stdlib advisories. Raise `api/go.mod`'s `go 1.25.0` directive to `1.26.0` so local toolchains match CI.
- Cheap patches in the same pass: `next` 16.2.11→16.2.12 with matching `eslint-config-next` and `@next/eslint-plugin-next` (currently pinned 16.0.5 — a real mismatch), `react`/`react-dom` 19.2.0→19.2.8, `gin-contrib/cors` 1.7.0→1.7.7.

**Nested `sharp`/`postcss` in Next — read before acting.** `npm audit` reports 3 high findings, all chaining to Next's bundled `postcss@8.4.31` and `sharp@0.34.5`. The app's own direct versions are patched. Exploitability is **low** because every `<Image>` sets `unoptimized` (21 usages, no custom loader), so Next's optimizer never runs. Do not accept npm's suggested "fix" of downgrading Next — it is nonsense. Resolve `C6`'s decision first: if you keep image optimization, add an npm `overrides` forcing `sharp >= 0.35` and re-run a production build; if you drop it, delete `images.remotePatterns` and the unused direct `sharp` dependency and the issue disappears.

**Acceptance** `govulncheck ./...` reports no module-level vulnerability. `npm audit --omit=dev` is either clean or has a written justification per remaining path. A "require code-owner review" rule actually matches files. `git ls-files web/.idea` is empty.

---

## 8. Phase 1 — hardening (H1–H14)

Next hardening release, after Phase 0 is deployed. Each item is smaller than a Phase 0 card, so they
are stated compactly: **what, where, why, done-when**.

### H1 — Consume magic-link tokens atomically
`api/internal/services/mentor_auth_service.go:203-247` and `admin_auth_service.go:147-182` query by
token, validate in application code, then clear with a second `UPDATE` — and both **log a clear
failure and still mint a session**. Two concurrent verifications can issue two 24-hour sessions; a
transient DB error leaves the link reusable. For admin links this is a privileged-session issue.
**Fix:** one `UPDATE ... SET login_token = NULL, ... WHERE login_token = $hash AND
login_token_expires_at >= NOW() RETURNING ...`, minting a JWT only from the returned row. Move the
expiry check into SQL. Apply the same shape to confirmation and resend, and store confirmation tokens
hashed like login tokens already are. Add a partial unique index on non-null token hashes.
**Done when:** two concurrent verifications produce exactly one session; an injected update failure
produces **no** session; the old token fails after a resend.

### H2 — Tighten the JWT contract and add session revocation
`api/pkg/jwt/jwt.go:65-116` issues HS256 with an issuer but validates only the HMAC *family* and never
requires issuer, audience, or subject consistency; missing claims can surface as a recovered 500.
Logout clears one cookie, and role/status changes do not revoke issued sessions.
**Fix:** require HS256 exactly; validate issuer, audience, expiry, issued-at, subject UUID and token
type. Use distinct audiences (or keys) for mentor vs admin — the middleware `token_type` check is good
but belongs in the JWT contract too. For admin requests re-read the current role/session version; for
mentor mutations verify current status. **One indexed session-version lookup is sufficient — do not
build a session platform.**
**Done when:** a token signed HS384 with the same secret is rejected; a token with a foreign issuer or
audience is rejected; demoting an admin invalidates their existing session on the next request.

### H3 — Make worker callbacks replay-safe
`api/internal/worker/repository.go:300-304` unconditionally sets `status='pending'` without touching
`status_changed_at`, so a replayed callback silently reopens a completed request with no timestamp
trace. `job_mentor_moderation.go:89-111` labels a check "idempotency" but actually reconciles the DB
*toward the replayed payload* — a stale `decline` arriving after a newer `approve` flips an active
mentor to declined and emails them a rejection. `FinalizeNewMentor` (`repository.go:200-213`) guards
`slug` and `login_token` but leaves `status` unguarded, so a replay re-drafts an active mentor and
mints a fresh confirmation token.
**Severity note:** this is **correctness, not urgent security** — the worker has no published ports, is
token-authenticated with a constant-time compare, and no retry mechanism exists. The realistic trigger
is two concurrent moderator actions.
**Fix:** add `AND status = $expected` (or an event id / `status_changed_at` comparison) to those writes
and require exactly one affected row; make the moderation check a real replay guard. **Do this before
any retry or queue mechanism is introduced** — retries are what turn these into recurring incidents.
**Done when:** replaying an event after the entity has advanced changes no state, credential, metric or
email; duplicate event ids are acknowledged without side effects.

### H4 — Replace review-by-primary-key authorization
See Chain B and `P14`. `request_id` is a bearer capability delivered in an email URL.
**Fix:** a separate random (≥32 bytes), hashed-at-rest, single-use, expiring token. Carry it in a POST
body or a dedicated header that is explicitly excluded from instrumentation — **never** a URL path or
query. Keep the request UUID internal.
**Scope depends on `D4`.** If the outstanding-invitation count is small (tens), ship the token in one
migration and **reissue** those invitations, invalidating the old links — simpler than a dual-read
window. If the count is large (dev showed 98% of completed requests), follow a proper expand →
dual-read → cutover → contract sequence; that complexity is earned at that scale.
**Done when:** two submissions with one token yield one success and one typed 409; expired or consumed
tokens reveal no request details; sentinel tests prove the token never reaches a URL, log, span or
analytics property.

### H5 — SHA-pin GitHub Actions and close the supply-chain gaps
All `uses:` reference mutable tags, including jobs holding the OIDC AWS role and `VM_SSH_KEY`
(`.github/workflows/deploy.yml:52,55,58,64,82`; also `checks.yml`, `ci-api.yml`, `ci-web.yml`). A
hijacked tag exfiltrates production credentials. `.github/dependabot.yml:7-25` has no
`github-actions` ecosystem (so pins would never be refreshed) and omits `infra/postgres-backup`.
`.github/workflows/deploy.yml:206-211` falls back to `ssh-keyscan` with only a warning when
`VM_SSH_HOST_KEY` is unset, i.e. trust-on-first-use over the connection it is authenticating.
**Fix:** pin every third-party action to a full 40-char SHA with a version comment; add the
`github-actions` ecosystem and the missing docker directory to Dependabot; make the host-key branch
fatal after confirming the secret is populated.
**Done when:** no `uses:` references a bare tag; a deploy with `VM_SSH_HOST_KEY` unset fails.

### H6 — Fix the gosec SARIF upload
`.github/workflows/ci-api.yml:29-30` sets workflow-level `permissions: contents: read` with no job
override, but `github/codeql-action/upload-sarif@v4` (`:121-125`) needs `security-events: write`, and
`continue-on-error: true` masks the failure. So gosec findings never reach the Security tab although
`README.md:49` claims they do.
**Fix:** scope `permissions: { contents: read, security-events: write }` to the `security` job; then
gate on new high/critical findings. **Done when:** a SARIF report appears in the Security tab.

### H7 — Remove direct Docker-daemon access and harden containers
`infra/docker-compose.yml:59` mounts the Docker socket into the internet-facing Traefik (`:ro` does
**not** make socket API calls read-only — an RCE in the only process on :80/:443 becomes host root).
`:65-76` gives cAdvisor `/`, `/var/run`, `/sys` and `/var/lib/docker`. No service sets `cap_drop`,
`read_only`, or `no-new-privileges`; Traefik, cAdvisor, Alloy and postgres-backup run as root (the
api/web images correctly drop to uid 1001).
**Fix:** front the socket with a filtering proxy exposing only the endpoints Traefik needs; restrict
cAdvisor's mounts to its documented minimum; add `no-new-privileges:true` and `cap_drop: [ALL]` broadly,
plus `read_only` with tmpfs where feasible. Also remove `--api.dashboard=true`
(`infra/docker-compose.yml:15`) — currently unrouted, but a loaded footgun.
**Done when:** a container-creation request through the endpoint Traefik sees is refused.

### H8 — Split database identities
`infra/docker-compose.yml:175-179` initialises `POSTGRES_USER=openmentor` as the image **bootstrap
superuser**, and `infra/.env.production.example:112,121` hands that same role to migrate, backend and
worker. No lesser role is created anywhere (no `docker-entrypoint-initdb.d`, no `CREATE ROLE` in
migrations). Superuser is **not required**: the only privileged DDL is `CREATE EXTENSION pgcrypto` and
`citext` (`api/migrations/000001_initial_schema.up.sql:4-5`), both *trusted* in PG13+ and installable by
a database owner. Today one SQLi or one compromised app container yields `COPY ... FROM PROGRAM`
(container RCE) and `pg_read_file`. The project already understands the pattern — `grafana_monitoring`
is a restricted role (`docs/runbooks/database-observability.md:50-54`) — it was just never applied to
the app.
**Fix:** create owner/migrator, DML-only `om_api`, DML-only `om_worker`, backup and monitoring roles;
point each process at its own. Scope the monitoring role down from `pg_read_all_data` (it currently
exposes PII). **Done when:** the API's credentials cannot `COPY ... FROM PROGRAM`, create roles, or drop
the schema, and migrations still run under the migrator identity.

### H9 — Serialize deployments
`.github/workflows/deploy.yml` has no `concurrency:` key (the other three workflows do). No lock exists
anywhere (`grep -i flock infra/*.sh` → nothing), and all three writers do a blind `cp .env .env.backup`
into the same directory (`deploy.yml:251`, `infra/deploy.sh:536-538`, `infra/rollback.sh:146`). Two
overlapping deploys therefore destroy the last known-good rollback target: deploy B backs up A's
unverified `.env`, and if B's health check fails it "rolls back" to A.
**Fix:** `concurrency: {group: production-deploy, cancel-in-progress: false}`; an `flock` in the shared
`infra/deploy-remote.sh`; timestamped `.env.backup.<epoch>` instead of one slot.
**Done when:** two simultaneous deploys serialize, and a rollback target is always a verified version.

### H10 — Make rollback honest about migrations
`infra/rollback.sh` only rewrites image tags (`:149-156`) and runs `docker compose up -d` (`:177`).
Because `migrate` shares the backend tag (`infra/docker-compose.yml:234`) and migrations are baked into
the image (`api/Dockerfile:61`), rolling back across a migration boundary makes golang-migrate's
`versionExists` check fail (`no migration found for version N`), `migrate` exits 1,
`service_completed_successfully` is unsatisfied, and `set -e` aborts — **leaving the bad version live**.
The hazard is already documented with a verified manual procedure at `infra/DEPLOYMENT.md:142-188`, but
`rollback.sh` gives no warning. Note the sharpest case involves no migration at all: rolling back only
the frontend to a pre-D30 build offers tag names the database no longer has.
**Fix:** have `rollback.sh` compare the target image's migration count against `schema_migrations.version`
and refuse with a pointer to that runbook section. Add the missing `000002_populate_tags.down.sql`.
Adopt an explicit expand/contract policy. **Do not** run lossy down-migrations automatically.
**Done when:** a cross-boundary rollback refuses up front with an actionable message.

### H11 — Make recovery procedures executable, and rehearse one
`docs/runbooks/postgres-backup-restore.md:31-32` and `docs/runbooks/postgres-16-to-18-upgrade.md:100-101`
run `aws s3 ls/cp` "on the VM", but the design deliberately removed the AWS CLI and credentials from
the VM (`infra/deploy-remote.sh:33-36`) while `docs/runbooks/provisioning.md:93` still installs
`awscli` — a direct contradiction. In a real incident, restore fails at step 2. Also
`docs/runbooks/provisioning.md:168` calls `/backup.sh` (actual path `/usr/local/bin/backup.sh`), and
`infra/DEPLOYMENT.md:170-175` plus the upgrade runbook assume a monorepo checkout on a VM that only
receives `infra/`.
**Fix:** rewrite so every command runs on the layout that actually exists (fetch the dump to a
workstation and `scp`, or officially keep awscli + backup credentials on the VM — decide and document
one). Then **actually rehearse a restore** into a scratch database. `D2` may depend on it, and `P8`
means nobody knows whether the dumps work.
**Done when:** the restore runbook executes verbatim against a VM containing only deployed artifacts,
and a restore has been performed successfully at least once.

### H12 — Add an infrastructure CI gate
Required checks only match `web/**` and `api/**`, so workflows, Compose, shell scripts, migrations,
Grafana rules and brand copies can change with no validation at all.
**Fix:** add a gate running Compose expansion (`docker compose config -q` for both overlays), the
`P10` env-allowlist check, `shellcheck`, `actionlint` (which would have caught `P5`), YAML/JSON
validation, a migration up/down apply against ephemeral Postgres, and Grafana rule validation. Keep it
consistent with the rule in `CLAUDE.md`: the gate owns fast checks exclusively; do not duplicate a
check that already lives in a deep workflow. **Done when:** a PR touching only `infra/` runs meaningful
checks and a deliberately broken Compose file fails CI.

### H13 — Fix alerts that cannot fire
`grafana/alerting/alert-rules.yaml:121,147` key on cAdvisor's `name` label, which this setup does not
expose — acknowledged in `grafana/README.md:81-84` — and both carry `noDataState: OK`, so they sit
permanently green. The memory threshold is `1073741824` (`:156`) while the **largest** `mem_limit` is
768m (`infra/docker-compose.yml:84`), so the kernel OOM-kills long before the rule could trigger. The
CPU rule (`rate(...) * 100 > 90`) is 90% of *one core* but described as "CPU above 90%". `:303` and
`:329` aggregate `db_client_*` across backend and worker with no service selector.
**Fix:** either fix cAdvisor (`/sys/fs/cgroup:ro`, `privileged`, `--docker_only`) and switch the memory
rule to a **ratio** of `container_spec_memory_limit_bytes`, or delete the two dead rules. Do not leave
alerts that cannot fire while `deploy.yml:339` tells operators to "monitor Grafana". Scope `db_client_*`
rules by service. **Done when:** every rule in the file can fire, and one has been proven to fire in a test.

### H14 — Per-binary configuration validation, and tests for the failure paths
`config.Validate()` omits S3 (the root cause of `P1`), SES, trigger URLs, base-URL/TTL constraints and
Discord, while worker and migrate can be forced to supply unrelated API settings.
**Fix:** `ValidateForAPI`, `ValidateForWorker`, `ValidateForMigrate`; fail startup on missing production
contracts. Add a small global JSON body cap with explicit image-endpoint overrides. Require
https-with-host on model `url` fields. Give `turnstile.Verify` a `ctx` and check `resp.StatusCode`
(`api/pkg/turnstile/turnstile.go:41-66` does neither, on the hot path of every public write); validate
the Turnstile hostname/action when configured.

**Tests — the highest-value gap.** Coverage is 37.6% (api) and 60% (web), but the real problem is that
*failure semantics* have no protection at all. In priority order:
1. `api/pkg/jwt` and both session middlewares (currently **no** tests).
2. An auth-service suite: expiry, single-use, concurrent verify, eligibility re-check.
3. `api/internal/repository/*` — **zero** tests today, including `ChangeSlug`'s advisory-lock
   transaction and the dynamic `Update` builder. Use ephemeral Postgres (a container in CI).
4. `web` auth verify/session routes — cookie forwarding, currently duplicated across two
   hand-maintained copies.
5. `ProfileForm` save (`P4`), reviews submit, request status transitions.

Note: some existing Go tests reconstruct production logic in local mocks rather than calling the real
service, which overstates their value. Prefer fakes at the repository boundary over re-implemented logic.

---

## 9. Phase 2 — correctness and performance (C1–C12)

- **C1 — Atomic profile writes.** `api/internal/services/profile_service.go:108-131` updates the mentor
  row and tags as two operations, and a tags failure is **logged while success is still returned**, so
  text and tag set can diverge silently. Also `if req.CalendarURL != ""` means a mentor can never
  *clear* their calendar link via the portal, while admin update always writes it — the two flows
  disagree. **Fix:** one transaction for row+tags; write `calendar_url` unconditionally.
- **C2 — Strict tag resolution on registration.** `registration_service.go:139-148` re-implements the
  tag loop non-strictly and silently drops unknown tags, the exact bug `resolveTagsStrict`
  (`services/tags.go:23-33`) documents at length and exists to prevent — and which already happened in
  production with the "Security" tag (see the comment in migration `000009`). **Fix:** use
  `resolveTagsStrict` and return a typed `tags_invalid` reason.
- **C3 — Gate contact creation in SQL.** `contact_service.go:96-105` inserts the request row *before*
  fetching the mentor, so draft/pending/declined mentors can be contacted via the API and an inactive
  mentor's calendar URL can be returned. **Fix:** `INSERT ... SELECT` gated on active/visible state;
  encode expected state in request-transition updates and require one affected row.
- **C4 — Indexed single-mentor lookup.** `mentor_repository.go:51-64` `GetByID` calls `GetAll` — the
  full `mentors × mentor_tags × tags` aggregate with a per-row `COUNT(*)` subquery — then linearly
  scans for `legacy_id`, on an endpoint rate-limited at 100 r/s. `legacy_id` is already uniquely
  indexed. **Fix:** add a `WHERE m.legacy_id = $1` variant (a 5-line copy of the existing slug/UUID
  queries).
- **C5 — Decode and upload the image once.** `api/pkg/s3storage/storage.go:191-233` decodes the same
  base64 5–6× per request and uploads identical bytes under `full`/`large`/`small` — aliases, not
  thumbnails (acknowledged in a comment). ~80 MB of transient allocations and 3× storage/egress per
  upload. **Fix:** decode once (pairs with `P3`), pass `[]byte` down, and store one canonical object
  until real variants are generated. Also align the client's 10 MB raw limit with the base64 body cap —
  today files of ~7.4–10 MB pass client validation then 413 (`web/src/pages/api/mentor/profile/picture.ts:45-47`).
- **C6 — Decide on image optimization.** All 21 `<Image>` usages set `unoptimized` and there is no
  custom loader, so `next.config.js:23-40` `remotePatterns` gates nothing and the direct `sharp`
  dependency is unused in the runner image, while mentor photos are served unresized. Either drop
  `unoptimized` (and add the `sharp` override from `P16`) or delete `remotePatterns` + `sharp`. Today
  you pay for both and benefit from neither. **This is a decision gate — see §11.**
- **C7 — Explicit metric route labels.** Follow through on `P13`: `withObservability('/api/...', handler)`
  with a compile-time label per route, migrated in batches of ≤5, rejecting empty or user-derived labels
  in development.
- **C8 — Bound catalog and OG rendering.** The homepage ships the full mentor list — including
  up-to-5,000-char `competencies` — into `__NEXT_DATA__` (`web/src/pages/index.tsx:47-61`), and
  `next.config.js:42-44` raises `largePageDataBytes` to 10 MB purely to silence the warning. `/mentors`
  already projects card fields; the homepage does not. The OG endpoint (`web/src/pages/api/og/mentor.tsx`)
  performs upstream fetches and a 1200×630 render with no method restriction, slug bounds, concurrency
  cap or application caching, and arbitrary `v` values multiply cache keys. **Fix:** project homepage
  props then remove the override; add `Cache-Control: s-maxage=60, stale-while-revalidate=300` to `/`
  and `/mentors` (as `sitemap.xml.ts:70` already does); bound the OG endpoint.
- **C9 — Static pages should be static.** Seven pages (`about`, `terms`, `privacy`, `faq`, `donate`,
  `bementor`, `migrate`) define a `getServerSideProps` whose only job is to write a log line, forcing
  SSR on every request with no CDN caching. There is no `getStaticProps` anywhere in the repo. The
  page-view "observability" this buys duplicates Faro, PostHog, GTM and the proxy access logs.
  **Fix:** delete those functions and let Next statically optimize the pages.
- **C10 — Fix the search haystack.** `web/src/components/hooks/useMentors.ts:52-65` searches
  `description`/`about`, but its only consumer (the homepage) fetches with `drop_long_fields: true`, so
  those are always empty — bio search silently never matches. Remove the fields or fetch them
  deliberately.
- **C11 — Reduce PII and secrets in logs.** Raw registration/recipient emails reach Go logs and Loki;
  PostHog error capture receives raw error strings. `api/cmd/migrate/main.go:46-53` `maskDatabaseURL` is a
  blind 20-byte prefix — safe for every username in the repo, but it silently starts leaking if the DB
  username drops below 8 characters. **Fix:** centralize structured redaction/hashing; parse the DSN
  with `net/url` and rebuild as `postgres://user:***@host/db`; sanitize errors before telemetry; add
  defence-in-depth redaction in Alloy. Complete the retention/erasure evidence the GDPR runbook lacks.
- **C12 — Scheduled-work safety and shutdown drains.** Add `cron.SkipIfStillRunning` and advisory-lock
  leases (`api/internal/worker/cron.go`), and pin `cron.WithLocation(time.UTC)` (`:60-66` currently runs
  "08:30 daily" in container-local time, so a base-image TZ change silently shifts jobs). Use atomic
  claims (`UPDATE ... RETURNING` / `FOR UPDATE SKIP LOCKED`) for migration consumers and verify image
  SHA-256 after copy. Give `api/pkg/analytics/tracker.go` a `Close(ctx)` that drains — up to 512 events
  are dropped per deploy — and align its `us.i.posthog.com` default with config's `eu.i.posthog.com`
  (a silent EU→US data path if the config default ever changes). Rotate `logs/frontend.log`
  (`api/internal/handlers/logs_handler.go:62-106` appends forever while lumberjack rotates the others).
  Also fix the source-map pipeline (`web/scripts/filter-sourcemaps.js:73` sets `mappings: ''`, producing
  maps that resolve nothing) and move `POSTHOG_PERSONAL_API_KEY` out of build-ARG layer metadata into a
  BuildKit secret mount.

---

## 10. Phase 3 — maintainability (M1–M7)

Only after the affected behaviour has regression coverage. Duplication was measured with `jscpd`:
94 duplicate blocks, 3.23% of lines overall; TypeScript 9.56%, Go 1.45%.

- **M1 — A typed BFF route helper.** ~37 routes in `web/src/pages/api/` repeat method gating, cookie
  forwarding, proxy invocation and error mapping. This is *why* `P7` existed in four of them. Extract
  one small typed helper; keep route-specific validation local.
- **M2 — Unify the mentor/admin auth stack (largest single win).** `web/src/lib/mentor-admin-api.ts`
  (290 lines) ≈ `admin-moderation-api.ts` (311) — identical `ApiError`, fetch wrapper, session cache and
  request/verify/logout logic. Four API route pairs differ by 7–13 lines. Two AuthContexts
  (`components/{admin-moderation/AdminAuthContext,mentor-admin/MentorAuthContext}.tsx`) and both
  login/callback pages mirror each other. Inside `web/src/lib/go-api-client.ts`, `request()` (`:89-125`)
  and `requestWithCookies()` (`:278-323`) duplicate the whole fetch/timeout body. ~1,500 lines that must
  be fixed twice — the 8 auth routes' hand-rolled 4xx logic (`P7`) is a symptom.
  **Fix:** one `apiRequest`/`ApiError` parameterized by base path; a `createAuthContext(sessionEndpoint)`
  factory; one parameterized verify/session/logout handler; collapse the two client methods with a
  cookie-forwarding flag. **Share the state-machine plumbing, not the domain permissions.**
- **M3 — Shared form module.** `selectStyles`, image validation, `isValidUrl`, `tagOptions` and
  `MAX_TAGS` are copy-pasted between `RegisterMentorForm.tsx` and `ProfileForm.tsx`, and ProfileForm's
  `selectStyles` copy is a **stale revision** — missing the redesign's focus ring, borders and
  transitions, so the profile-edit tag select visibly doesn't match. Extract to
  `web/src/components/forms/shared.ts`, keeping the newer styles. Extract shared schema and field groups
  before changing behaviour.
- **M4 — Shared request-list code.** `web/src/pages/mentor/index.tsx:31-54` and `past.tsx:33-56` have
  byte-identical `filterRequests`/`sortRequests` and a ~70% identical body. Move the helpers into the
  existing `components/mentor-admin/utils.ts` and extract a shared shell.
- **M5 — One calendar embed.** `Koalendar.tsx` and `CalendlabWidget.tsx` are identical except iframe
  height; both bypass the `safeHttpUrl` guard the plain-link branch uses, and both append `?embed=true`
  even when the URL already has a query string. Merge into `CalendarEmbed({ url, height })` applying
  `safeHttpUrl` and using `URL`/`searchParams`. **Also reconcile a real mismatch:** the CSP `frame-src`
  allows `calendlab.ru` while the detector matches `calendlab.com`
  (`api/internal/models/mentor.go:243`), so calendlab iframes are currently CSP-blocked.
- **M6 — Consolidate the Go auth primitives.** `mentor_auth_service.go` (317 lines) and
  `admin_auth_service.go` (234) are structural mirrors, as are the two handlers (marked
  `//nolint:dupl`) and the two session middlewares. Four near-identical `crypto/rand` token generators
  exist (`generateLoginToken`, `generateAdminLoginToken`, and one `generateConfirmationToken` each in
  `api/internal/worker/job_new_mentor_watcher.go:26` and
  `api/internal/services/mentor_confirmation_service.go:662`). The 22-column mentor SELECT list appears
  verbatim 3× (`mentor_repository.go:111-132, 509-531, 542-564`) and `GetByLoginToken` (`:400-482`)
  hand-rolls a fourth variant with its own 60-line scan instead of reusing `ScanMentor`.
  `client_request_repository.go` shows the right pattern with a shared `clientRequestSelect` const.
  **Consolidate the cryptographic consume primitive and the SELECT const; keep the distinct roles,
  signing contracts and responses.**
- **M7 — Delete dead code, then guard against regrowth.** Verified unreachable (`deadcode`, `knip`, and
  manual grep): `api/internal/middleware/rate_limit.go:52` `RateLimiter.Stop`; `api/pkg/errors`
  `ErrConflict`, `ErrUnauthorized`, `ErrInternal`, `ErrAccessDenied`, `AccessDeniedError`,
  `InternalError`; `api/pkg/logger` `LogError`; `api/pkg/tracing` `Tracer`, `StartSpan`;
  `api/pkg/db/pool.go:73-77` `Close`; `mentor_repository.go:366-369` `GetAllTags`;
  `api/pkg/metrics/metrics.go:141-147` `MentorProfileViews` (registered, never incremented, declared
  with an empty label vec); `api/pkg/email/templates` `Names()`; the commented-out webhook block at
  `api/internal/services/profile_service.go:180-188`. Web: unreferenced barrels
  (`web/src/{config,lib,server}/index.ts`); `faro.ts:148-190` `getFaro`/`pushEvent`/`pushError`/`setUser`;
  `logger.ts:142-153` `createContextLogger`; `with-observability.ts:107-140` `measureAsync`/`measureSync`;
  `with-ssr-observability.ts:79-115` `withStaticPropsObservability` (there is no `getStaticProps` in the
  repo); `mentor-admin-api.ts:118-120` `isSessionValid`; `server/mentors-data.ts:39-58`
  `getOneMentorById`/`getOneMentorByUuid` plus their `GoApiClient` backers; `config/constants.ts:24`
  `CACHE_REFRESH_INTERVAL` (an Airtable-cache leftover); unused mentor-filter state
  (`selectedNoSessions`, `selectedNewMentor`); the `start2`/`dev2` npm scripts. Dead config knobs:
  `DB_WORK_OFFLINE` (nothing implements an offline mode — `db.NewPool` still fatals on an empty URL) and
  `DatabaseConfig.MaxConns/MinConns` (hardcoded 20/2 while the worker's cap *is* env-driven).
  Stale files: `web/Dockerfile:113-115` commented `COPY` and the line-131 comment claiming
  `start-server.js` while `CMD` runs `server.js`.
  **Then add `knip` to CI** so dead exports cannot re-accumulate.
  Two small correctness fixes to fold in: `mentor_repository.go:157-190` `Update()` generates invalid
  SQL for an empty updates map (`SET , updated_at = ...`) and would double-assign if `updated_at` were
  passed (it is in the allowlist) — guard `len(updates)==0` and drop `updated_at` from the allowlist;
  and `api/internal/models/mentor_client_request.go:214-216` hardcodes
  `https://openmentor.io/reviews/new?...` inside a scan function, so staging links point at production —
  move URL construction to the service layer using `cfg.Server.BaseURL` (the worker already does this
  correctly).
  **Documentation drift** to correct in the same sweep (live runbooks only — never `docs/migration/`):
  `infra/README.md:67` says the backend runs `/app/main` (actually `/app/openmentor-api`);
  `docs/runbooks/database-observability.md:16` pins Alloy `v1.17.1` (actually `v1.18.0`);
  `docs/runbooks/postgres-backup-restore.md:109` references an `api/certs/` directory that does not
  exist (TLS uses `sslrootcert` in the DSN); `infra/docs/troubleshooting.md:240-307` diagnoses ACME
  HTTP-01/port-80 causes although production uses DNS-01 via Cloudflare and never mentions
  `CLOUDFLARE_DNS_API_TOKEN`, and uses `docker-compose` v1 syntax throughout; `infra/DEPLOYMENT.md:15-17`
  and `infra/ENVIRONMENT_VARIABLES.md:55-61` contain garbled sentences mixing the retired pull-only-IAM
  flow with the current token-over-ssh flow; `web/CLAUDE.md` lists a nonexistent
  `components/mentors/FilterGroupDropdown.tsx`. Reconcile the
  `DISCORD_MENTORS_PRIVATE_INVITE_LINK` contradiction (`infra/.env.example:113` says leave it empty;
  `api/internal/worker/jobs.go:57-60` says it is required because the template renders the section
  unconditionally) — one of the two is wrong.
  Add the env vars that `api/config/config.go` reads but neither infra template documents: `BASE_URL`,
  `ALLOWED_CORS_ORIGINS`, `TRUSTED_PROXIES`, `JWT_ISSUER`, `SESSION_TTL_HOURS`,
  `LOGIN_TOKEN_TTL_MINUTES`, `COOKIE_DOMAIN`, `COOKIE_SECURE`, `WORKER_DB_MAX_CONNS`. All have
  production-safe defaults today, but `infra/ENVIRONMENT_VARIABLES.md:5-7` calls the templates
  authoritative and a `--staging` deploy silently inherits production `BASE_URL`/CORS.
  Finally, `.github/workflows/deploy.yml:92-97` passes only 5 frontend build-args while
  `infra/deploy.sh:331-353` passes ~20 — a workflow-deployed frontend bakes empty PostHog/Faro/analytics
  config and skips sourcemap upload, so client RUM silently dies depending on which path shipped.
  Either pass the same args or document the gap.

**Routine dependency maintenance** (no security driver; do after the above): Tailwind 3→4 (a config and
build-pipeline migration with visual-regression risk), Jest 29→30, ESLint 9→10, `@types/node` ^22→^26
(currently mismatched against a Node 26 runtime), `zap` 1.26→1.28, `prometheus/client_golang` 1.19→1.24
(retest `/metrics` after — escaping-scheme changes landed in between), `posthog-go` 1.10→1.22,
`pgx` 5.9→5.10, aligning `otelhttp` 0.63 with `otelgin` 0.69, and `checkout@v5`/`setup-node@v5`/
`build-push-action@v6` alongside `H5`'s SHA-pinning. `patrickmn/go-cache` is unmaintained but trivially
used and CVE-free — fine to keep, or removed for free by simplifying the tags cache
(`api/internal/cache/tags_cache.go` uses a whole dependency for one key, wrapped in a second mutex,
drops the caller's context in `refresh()`, and returns the cached map by reference; a ~30-line struct
would be simpler). `uuid ^14` is legitimate, not a typo. Consolidating the three client analytics
stacks is worthwhile: `react-gtm-module` is unmaintained, and `GTM-5GLW4WPS` is hardcoded in
`web/src/pages/_app.tsx:97-105` and initializes **regardless** of
`NEXT_PUBLIC_ANALYTICS_PROVIDER`, so the documented "none" switch does not disable tracking and
self-hosters ship data to openmentor's container — move it to `NEXT_PUBLIC_GTM_ID` and gate it.

---

## 11. Decision gates needing the owner

These need a human decision or production access. A developer should **not** guess at them.

1. **How many outstanding review invitations exist in production?** Run `D4`. This selects `H4`'s
   approach: a small count (tens) means ship the new token and reissue invitations; a large count (dev
   showed 98% of completed requests) means a proper expand → dual-read → cutover sequence.
2. **Does a restorable pre-corruption `pg_dump` exist?** `D2` may need one to recover clobbered prices,
   and `P8` shows backup failures have been silent — so this is genuinely unknown until checked. Verify
   a dump exists in S3 **and** that it restores into a scratch database. If none restores, price recovery
   falls back to recomputing from the getmentor import source, and `P8`/`H11` become urgent.
3. **What are the Grafana Cloud and PostHog retention windows, and does the plan support deletion?**
   Determines whether `P14`'s purge is actionable or whether you are waiting out retention. The specific
   capability to check is PostHog **person** deletion, for the `request:<uuid>` distinct_ids.
4. **Keep Next image optimization, or drop it?** (`C6`, and it gates the `sharp` decision in `P16`.)
   Currently all 21 `<Image>` usages set `unoptimized` while the backend pre-generates size variants —
   so you pay for the config and the dependency and get no benefit. Either is defensible; pick one.
5. **Ko-fi account** — confirm ownership and the correct URL before the launch event (`P16`).
6. **Single-VM availability** remains a documented, accepted risk, and is the right call at this scale.
   Worth one rehearsal of complete VM-loss recovery plus external uptime monitoring — the latter also
   compensates for `P15` until it is fixed.

---

## 12. Explicitly out of scope

Both source audits independently warned against the same over-corrections. Restating them, because
each is a plausible-sounding way to turn this plan into a rewrite:

- **Do not introduce microservices or Kubernetes.** Nothing here is caused by the monolith or the
  single-VM deployment. Clear service contracts, least privilege, durable work and tested recovery
  deliver far more for far less complexity.
- **Do not adopt a durable job queue (e.g. River) as part of Phase 0.** The lossy fire-and-forget
  goroutines are a real defect (Chain A), and a Postgres-backed queue with a transactional outbox is the
  right long-term answer. But it is a multi-day architectural change with its own failure modes, and the
  failure that actually matters — a permanently locked-out mentor — is removed by `P2`'s reconciliation
  cron (~50 lines, no new dependency), which also covers worker downtime and deploy restarts in a way a
  queue alone does not if the enqueue itself is lost. Revisit once job variety or volume justifies it,
  as one deliberate project. **Ordering matters: `H3`'s compare-and-swap guards must land before any
  retry mechanism exists**, because retries are what turn an unguarded write into a recurring incident.
- **Do not write a custom queue.** If you do adopt one, use a maintained Postgres-backed library.
- **Do not build a generic BFF, repository, or session framework.** Extract only repeated, stable
  mechanics behind small typed helpers (`M1`, `M2`). Do not merge the mentor and admin *domain* services
  to chase a duplication percentage.
- **Do not edit already-applied migrations.** Add new ones; document irreversible boundaries.
- **Do not run lossy down-migrations automatically during rollback.** Prefer roll-forward; require a
  fresh tested backup and an explicit operator decision for an incompatible migration.
- **Do not add speculative indexes.** `H10`/`C4` name specific, query-aligned ones. Anything beyond
  those needs representative `EXPLAIN ANALYZE` evidence first.
- **Do not add more logging or telemetry as a substitute for durable state and tested recovery** — and
  note `P14`: telemetry is currently a liability, not only an asset.
- **Do not "fix" `docs/migration/`.** It is a historical record of the getmentor→openmentor fork.

---

## 13. Appendix: finding index

Severity is post-verification. "Verified" means reproduced by running code.

| ID | Title | Severity | Effort | Area | Verified |
|---|---|---|---|---|---|
| D1 | Locked-out mentors — detection | — | XS | db | ✅ |
| D2 | Price corruption — detection | — | XS | db | ✅ |
| D3 | Email-injection — detection | — | XS | db | ✅ |
| D4 | Outstanding review capabilities — detection | — | XS | db | ✅ |
| P1 | Nil S3 client kills the API process | critical | S | api | ✅ |
| P2 | Mentors locked out, told their link was sent | critical | S | api | ✅ |
| P3 | Image bomb OOM, no captcha on one path | critical | S | api | ✅ measured |
| P4 | Profile save destroys the mentor's price | high | S | web | ✅ |
| P5 | Deploy workflow shell injection | critical | S | ci | ✅ |
| P6 | Unescaped user input in SES email templates | high | S–M | api | ✅ |
| P7 | Contact 4xx→500, Turnstile never reset | high | S | web | ✅ |
| P8 | Backup failures are completely silent | high | S | infra | ✅ |
| P9 | Backup template restart-loop rolls back deploys | high | XS | infra | ✅ |
| P10 | Every container gets every secret | high | M | infra | ✅ |
| P11 | Resend limited to 2 per 5 min platform-wide | high | S | api | ✅ |
| P12 | Support runbook emails nearly every secret | high | XS | docs | ✅ |
| P13 | Unbounded metric cardinality | medium | XS | web | ✅ measured |
| P14 | Review capability + tokens in telemetry | high | S | web+api | ✅ |
| P15 | Broken edge reports a successful deploy | medium | XS | ci | ✅ |
| P16 | Launch hygiene + security dependency patches | mixed | XS–S | repo | ✅ |
| H1 | Magic-link consumption not atomic, fails open | high | S | api | code-read |
| H2 | JWT contract loose; no session revocation | medium | M | api | ✅ partial |
| H3 | Worker callbacks not replay-safe | medium | S | api | code-read |
| H4 | Review authorization by primary key | high | M | api+web | ✅ |
| H5 | Mutable action tags; SSH TOFU; Dependabot gaps | high | S | ci | code-read |
| H6 | gosec SARIF upload cannot succeed | medium | XS | ci | code-read |
| H7 | Docker socket in Traefik; no hardening flags | high | M | infra | code-read |
| H8 | App runs as Postgres bootstrap superuser | medium | M | infra | ✅ |
| H9 | Deployments can interleave, destroying rollback | medium | S | ci | ✅ |
| H10 | Rollback inoperable across a migration boundary | medium | S | infra | ✅ |
| H11 | Recovery runbooks cannot run as written | high | S | docs | ✅ |
| H12 | No infrastructure CI gate | medium | M | ci | code-read |
| H13 | Alerts that cannot fire | medium | S | grafana | ✅ |
| H14 | Per-binary config validation + test coverage | high | L | api | ✅ partial |
| C1–C12 | Correctness and performance | mixed | — | — | mixed |
| M1–M7 | Maintainability, dead code, docs | low | — | — | ✅ measured |

**Refuted — do not spend time on these** (see §4): the "internal API token in trace headers" leak
(latent only — just delete the dead config); the DSN-mask password leak (theoretical at every username
in the repo); the magic-link leak *via logs and PostHog* (both already mitigated; the OpenTelemetry
`url.query` leak is real and is `P14`).

---

## Change log

| Date | Change |
|---|---|
| 2026-08-02 | Two independent audits produced (external advisor; internal). |
| 2026-08-03 | Audits reconciled; conflicting claims re-verified against source; corrections recorded in §4. |
| 2026-08-03 | Operating context corrected — the system is **live**; rotation, incident response and additive-migration constraints added. |
| 2026-08-03 | Every P-item reproduced by executing code. `P3` reachability upgraded (un-captcha'd path found); `P1`/`P2` false-success responses documented; `P6`'s escape-at-the-boundary alternative empirically refuted; `P7` severity wording softened; a schema error in this plan's own SQL found and fixed. |
| 2026-08-03 | Rewritten as a standalone, self-contained plan with a reproduction harness in `verification/`. |
| 2026-08-03 | Round-2 review of the Phase 0 fixes found five defects in **this document** — recorded in §4.1, and the text changed where following it would have caused harm. Instructions changed: `D1`'s repair (blanket finalization replay → the `D1b`-classified procedure in `data-repair.md`; the handler is not idempotent), `D1`'s acceptance ("`D1` returns zero rows" → `D1b` returns zero `stuck_registration` rows), the embedded `D3` query (added `preferred_contact` and `price`; `\b`→`\y`), `P6` step 4 (naive `template.HTML` wrapping → rebuild the fragments as `html/template` templates). Claims corrected: `P2` touches four `ScanMentor` queries, not three; `P4`'s `experience` field has the identical defect and is in scope. |
