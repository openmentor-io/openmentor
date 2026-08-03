-- ===========================================================================
-- OpenMentor — 2026-08 audit diagnostics D1–D4
--
-- READ-ONLY and safe to run verbatim against production: SELECTs only, no
-- DDL, no writes, no VACUUM/ANALYZE, no EXPLAIN ANALYZE. The session is
-- pinned read-only below so a stray write aborts instead of landing. Only
-- ordinary ACCESS SHARE locks are taken (unavoidable for any SELECT); nothing
-- here blocks a writer.
--
-- D1-D4 and the SUMMARY run inside ONE read-only REPEATABLE READ transaction,
-- so the summary counts always describe the same instant as the detail rows
-- above them. Consequence for running single checks by hand: the BEGIN is near
-- the top and the COMMIT is at the bottom, so copy a query out on its own
-- rather than pasting a fragment that starts mid-transaction.
--
-- Every column referenced here was checked against api/migrations/*.up.sql and
-- the whole file was executed against a scratch database built from those
-- migrations. Do not "fix" a column name from the Go models — `reviews` has
-- NO `request_id` and NO `review_text`; the real columns are
-- `client_request_id`, `mentor_review`, `platform_review` and `improvements`.
--
-- Run it:
--   ./diagnostics.sh                     # wrapper; writes a timestamped report
--   docker exec -i openmentor-postgres \
--     psql -U openmentor -d openmentor -X -f - < diagnostics.sql   # by hand
--
-- Output ends with a '## SUMMARY' block; diagnostics.sh reprints just that.
--
-- THE OUTPUT CONTAINS PERSONAL DATA (names, email addresses, request text).
-- Treat a saved report like a database export: keep it off shared drives and
-- delete it when you are done.
-- ===========================================================================

-- QUIET drops psql's "SET"/"Timing is off." command chatter so the report is
-- readable. Errors are NOT suppressed by it (verified), and ON_ERROR_STOP
-- still aborts on the first one.
\set QUIET on
\set ON_ERROR_STOP on
\pset pager off
\timing off

-- Safety rails. Session-local; they vanish when psql exits. Set BEFORE the
-- transaction opens, so they are plain session settings and not something a
-- rollback could undo halfway through.
SET default_transaction_read_only = on;   -- any write in this session fails
SET statement_timeout = '120s';
SET lock_timeout = '5s';
SET idle_in_transaction_session_timeout = '120s';

-- Provenance for the report. \conninfo prints database/user/host/port and
-- never a password.
\conninfo

-- ---------------------------------------------------------------------------
-- ONE SNAPSHOT FOR THE WHOLE REPORT.
--
-- Without this every statement below would run in its own transaction
-- (psql is in autocommit), so the SUMMARY counts would be taken from a
-- different point in time than the detail rows above them — a profile save, a
-- request finalization or a review submission landing mid-run makes the two
-- disagree, and the operator cannot tell which half is stale. Verified on a
-- scratch database: with autocommit, a concurrent INSERT between two counts is
-- visible to the second one; inside this transaction it is not.
--
-- REPEATABLE READ (not SERIALIZABLE) is enough: we only read, so there is no
-- write skew to serialize and no chance of a serialization failure forcing a
-- retry. READ ONLY is belt-and-braces on top of the session setting above.
--
-- It takes only ACCESS SHARE locks and blocks no writer. It does hold back
-- the vacuum horizon for as long as it runs, which is why statement_timeout
-- stays at 120s: the whole report is a handful of seconds on this data size,
-- and no single query can run away.
--
-- If any statement fails, ON_ERROR_STOP aborts psql with the transaction still
-- open; disconnecting rolls it back. Nothing is written either way.
-- ---------------------------------------------------------------------------
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;


-- ===========================================================================
-- D1 — mentors locked out of their own account
--
-- DETECTS  Mentor rows whose `sort_order` is NULL.
--
-- MEANS    models.ScanMentor (api/internal/models/mentor.go:114,142) scans
--          `sort_order` into a non-pointer int (`Mentor.SortOrder int`), so
--          pgx fails the WHOLE row on NULL. Four query paths select the column
--          raw and go through that scan, so one NULL breaks all of them:
--            GetByEmail (:375-393)              -> magic-link login
--            FetchSingleMentorFromDB (:542-567) -> the PUBLIC PROFILE PAGE at
--                /<slug>, via GetBySlug (:70). NO status filter, so this
--                breaks for a draft, inactive or active mentor alike.
--            FetchAllMentorsFromDB (:508-538)   -> the public catalog. Filtered
--                to status='active', so only an ACTIVE row with a NULL breaks
--                the catalog — but then it breaks it for everyone.
--            fetchMentorByUUIDFromDB (:110-135) -> mentor-by-UUID lookups
--          (GetByLoginToken at :400 scans into *int and is NOT affected.)
--          On the login path the endpoint answers with the same
--          enumeration-safe "we sent you a link" either way, so the person
--          waits for an email that was never generated and never complains.
--          They are also unmoderated and got no confirmation mail.
--          Root cause: registration commits the row with sort_order = NULL
--          and dispatches finalization as a fire-and-forget goroutine. If
--          that call is lost, nothing retries it.
--
-- NOTE     A NULL here is not always a lost registration — mentors imported
--          by infra/migration/migrate-mentors.js carry `sort_order` over from
--          getmentor.dev verbatim, so an imported row can be NULL and land in
--          status 'inactive'. D1b separates the two cases. This distinction
--          is load-bearing: the repair for one case DAMAGES the other. Read
--          data-repair.md before touching anything.
--
-- DO       See data-repair.md §D1. Short version: ship the COALESCE fix, then
--          re-run finalization ONLY for rows D1b classifies as
--          "stuck_registration". Consider emailing those people.
-- ===========================================================================
\echo ''
\echo '## D1 — mentors with sort_order IS NULL'

SELECT id, email, name, status, created_at
FROM mentors
WHERE sort_order IS NULL
ORDER BY created_at;

-- D1b — triage. `airtable_id` holds 'getmentor:<old legacy_id>' for imported
-- profiles (api/migrations/000006 comment; migrate-mentors.js
-- MIGRATION_MARKER_PREFIX). Only 'stuck_registration' rows may be re-finalized.
\echo ''
\echo '## D1b — D1 rows classified (only stuck_registration is safe to re-finalize)'

SELECT
    id,
    email,
    name,
    status,
    created_at,
    CASE
        WHEN airtable_id LIKE 'getmentor:%' THEN 'imported_profile'
        WHEN status = 'draft' AND activated_at IS NULL THEN 'stuck_registration'
        ELSE 'other_investigate'
    END AS classification
FROM mentors
WHERE sort_order IS NULL
ORDER BY classification, created_at;


-- ===========================================================================
-- D2 — mentor prices silently overwritten with 'Free'
--
-- DETECTS  (a) Mentors whose price is now 'Free' and whose row has been
--              updated at least once since creation.
--          (b) Exposure: how many mentors currently hold a price that is NOT
--              one of the six options the profile <select> offers.
--
-- MEANS    web/src/components/forms/ProfileForm.tsx renders an UNCONTROLLED
--          <select {...register('price')} defaultValue={mentor.price}> with no
--          placeholder option. When the stored price is not in the option list
--          (web/src/config/filters.ts:82-85 -> Free, $50, $100, $150, $200,
--          Negotiable) the browser selects the FIRST option — 'Free' — and
--          react-hook-form reads that DOM value at mount. Saving anything
--          else on the profile then writes 'Free' over the real price.
--          Every row in (b) is one profile save away from that.
--          (a) is a CANDIDATE list, not proof: a mentor who genuinely chose
--          'Free' and later edited their profile also appears.
--
-- DO       See data-repair.md §D2. This is NOT cleanly recoverable from the
--          database — the old value was overwritten in place with no audit
--          trail. FIX THE FRONTEND BUG BEFORE RESTORING ANY VALUE, or the
--          next save re-corrupts it.
-- ===========================================================================
\echo ''
\echo '## D2a — price = Free on a row that has been updated (candidate clobbers)'

SELECT id, email, name, price, created_at, updated_at
FROM mentors
WHERE price = 'Free'
  AND updated_at > created_at
ORDER BY updated_at DESC;

-- Exposure. '' (empty string) is deliberately counted: it is not one of the
-- select's options either, so it triggers the same first-option fallback.
--
-- NULL IS COUNTED TOO, and an earlier `IS NOT NULL` filter here hid those
-- rows. `price` is plain nullable TEXT (migration 000001, `price TEXT`) with no
-- CHECK and no default, so NULL is a value the schema permits and NULL is not
-- one of the six options either. Its consequence is in fact WORSE than an
-- off-list string, and it is the D1 failure on a second column: models.ScanMentor
-- scans `price` into a non-pointer `Mentor.Price string` (models/mentor.go:23,138),
-- so pgx fails the WHOLE row. Verified with pgx v5.9.2 against a scratch
-- database: scanning a NULL `price` into *string returns
--   can't scan into dest[N] (col: price): cannot scan NULL into *string
-- while COALESCE(price,'') on the same row returns ''. So a NULL price breaks
-- GetByEmail (login), the public profile page and the catalog exactly as a NULL
-- `sort_order` does — and on the paths that DO COALESCE it (the admin/moderation
-- reads at mentor_repository.go:607,661 and the worker's GetJobMentorByID) it
-- surfaces as '', which is off-list, which is one save from 'Free'.
-- `experience` (Mentor.Experience string) has the identical shape, which is why
-- D2d has always counted NULL. Both filters now agree.
\echo ''
\echo '## D2b — mentors holding a price the profile select cannot represent'

SELECT count(*) AS off_list_prices
FROM mentors
WHERE price IS NULL
   OR price NOT IN ('Free', '$50', '$100', '$150', '$200', 'Negotiable');

\echo ''
\echo '## D2c — the actual off-list values (input for the recompute in data-repair.md)'

-- COALESCE so a NULL is a visible '<NULL>' bucket rather than a blank cell an
-- operator reads as the empty string. Same shape as D2d.
SELECT COALESCE(price, '<NULL>') AS price, count(*) AS mentors
FROM mentors
WHERE price IS NULL
   OR price NOT IN ('Free', '$50', '$100', '$150', '$200', 'Negotiable')
GROUP BY 1
ORDER BY mentors DESC, 1;

-- ---------------------------------------------------------------------------
-- D2d — THE SAME BUG ON A SECOND FIELD (not in the original audit plan)
--
-- DETECTS  Mentors whose `experience` is not one of the three options the
--          profile <select> offers.
--
-- MEANS    ProfileForm.tsx:404-409 renders experience with the identical
--          defect as price — uncontrolled `{...register('experience')}` +
--          `defaultValue={mentor.experience}` and NO placeholder option. The
--          options are web/src/config/filters.ts `experience` values:
--          '2-5', '5-10', '10+'. Anything else (including '') therefore falls
--          back to the FIRST option, '2-5', on the next profile save.
--          `experience` is free TEXT in the schema (no CHECK constraint), and
--          the importer's `mapExperience` (migrate-mentors.js:412-417) keeps
--          unknown source values verbatim — so imported profiles are the most
--          likely holders of an off-list value.
--          RegisterMentorForm.tsx:697-711 DOES have `<option value="">`, so
--          registration is not affected; only the profile edit form is.
--
-- DO       Fix it in the same change as the price select (audit item P4);
--          otherwise it stays a live corruption path. There is no recovery
--          source for a clobbered `experience` beyond the same pg_dump /
--          re-import route as price — see §D2 in data-repair.md.
-- ---------------------------------------------------------------------------
\echo ''
\echo '## D2d — mentors holding an experience the profile select cannot represent'

SELECT COALESCE(experience, '<NULL>') AS experience, count(*) AS mentors
FROM mentors
WHERE experience IS NULL
   OR experience NOT IN ('2-5', '5-10', '10+')
GROUP BY 1
ORDER BY mentors DESC, 1;


-- ===========================================================================
-- D3 — has the email HTML injection been exercised?
--
-- DETECTS  Stored free text that contains markup, and calendar URLs whose
--          scheme is not https.
--
-- MEANS    These fields are interpolated into SES email templates WITHOUT
--          escaping, and the mail is sent from the DKIM-signed openmentor.io
--          domain. Verified sinks:
--            client_requests.description -> `request_details` in new-request /
--              new-request-calendly (to the mentee) and `mentee_request` in
--              new-request-mentor (to the mentor)
--            client_requests.name        -> `first_name` / `mentee_name`
--            client_requests.preferred_contact -> `mentee_contact` in
--              new-request-mentor (job_new_request_watcher.go:109-123, via the
--              "not provided" fallback). Mentee-supplied free text: the API
--              binding is `omitempty,max=100` (models/contact.go), no charset
--              or format check, and the template drops it into a <div>.
--            mentors.name                -> `mentor_name` / `first_name`
--            mentors.price               -> `request_price` in new-request AND
--              new-request-calendly (job_new_request_watcher.go:99). Mentor-
--              supplied free text: `required,max=100` (models/profile.go) with
--              no option-list check server-side, and the template drops it into
--              a <div>. This is the field D2 is about — the same column that is
--              too free-form for the profile <select> is also an email sink.
--            mentors.calendar_url        -> `calendly_url` in
--              new-request-calendly (job_new_request_watcher.go:102-104)
--            mentors.moderation_note     -> `reviewer_note` in
--              new-mentor-returned (job_mentor_moderation.go:166-174, via
--              JobMentor.ModerationNote). The moderator's own "please fix this"
--              note, written by the API's return-to-draft action
--              (mentor_repository.go ReturnMentorToDraft) and mailed to the
--              mentor unescaped. The remediation plan's P6 sink table lists it
--              (`reviewer_note`, max 2000); D3 used to skip straight from
--              calendar URLs to the review fields, so a clean D3 said nothing
--              about it. Its author is a moderator, not an attacker, so a hit
--              here is far more likely to be prose about markup than an
--              injection — but "we never checked" is not an answer to "was this
--              exercised?".
--            reviews.mentor_review       -> `review_text` in new-review
--              (truncated to 500 chars, job_process_review.go)
--          reviews.platform_review and reviews.improvements are stored and
--          counted in analytics but reach NO email template — a hit there is
--          worth knowing about but is not an email-injection vector.
--
--          NOT a sink, deliberately: client_requests.level (-> `mentee_level`
--          in new-request-moderator). It is bound `oneof=Junior Middle Senior
--          Manager 'Manager of managers' C-level` (models/contact.go), so it
--          cannot carry markup, and adding it would only produce noise.
--
--          A hit is NOT proof of an attack. Mentors legitimately write "<3"
--          or paste HTML into their bio. What IS close to proof: an <a href>
--          inside a person's NAME, or a javascript:/data: calendar URL. Those
--          turn this from a code question into a disclosure question — an
--          attacker-controlled link was mailed from your domain, and you would
--          need to work out who received it (check SES sending logs for the
--          affected request/review, and the mentor's inbox with their help).
--
-- DO       Escape at the template boundary (audit item P6) and validate
--          calendar_url's scheme AND its characters. If any name/calendar_url
--          row is a real injection, treat it as an incident, not a backlog item.
--
-- CALENDAR_URL  The scheme test alone is not enough, and this is the sharpest
--          gap in this query. calendar_url lands inside an attribute —
--          href="{{calendly_url}}" — so a value that keeps the https:// prefix
--          and then closes the attribute is a complete injection that a
--          scheme-only predicate calls clean:
--            https://evil.example/x"><img src=x onerror=alert(1)>
--          Verified on a scratch database: with only `!~* '^https://'` that row
--          returns 0 rows; with the character class below it is found.
--          The class is `[<>"'` + backtick + [:space:]]. Every one of those
--          either terminates an HTML attribute or opens a tag, and none of them
--          is legal in a URI (RFC 3986 excludes <, >, ", backtick and space;
--          `'` is a sub-delim no calendar URL uses), so it does not fire on
--          real Calendly links — `?month=2026-08&back=1`, `#frag`, percent-
--          escapes and the other sub-delims all pass. `[:space:]` rather than
--          `\s` inside the bracket expression: unambiguous in every regex
--          flavour, which matters in a file operators copy into other tools.
--
-- REGEX    EVERY free-text sink uses the same deliberately permissive
--          predicate, `<\s*[a-z]` — '<' followed by a letter.
--
--          `description` used to carry a tag allowlist instead,
--          `<\s*(a|img|div|table|script|style)\y`, which is a denylist wearing
--          an allowlist's clothes: `<form>`, `<iframe>`, `<svg>` and `<input>`
--          are not on the list and every one of them reaches the same
--          unescaped `{{request_details}}` / `{{mentee_request}}` interpolation
--          (job_new_request_watcher.go:98,124) in DKIM-signed mail. An
--          unauthenticated contact request using any of them was reported
--          CLEAN, so D3 would have concluded the injection was never exercised.
--          Verified on a scratch database: '<iframe src=x>' does not match the
--          tag list and does match `<\s*[a-z]`. D3 answers "was this ever
--          exercised?", where a false negative ends the investigation and a
--          false positive costs one eyeballed row — so the permissive form is
--          the right trade in every one of these fields, not just the short
--          ones.
--
--          Expect benign hits and read them, do not tune them away. In long
--          prose: 'if a < b' matches (`\s*` spans the space). In
--          preferred_contact: an address written '<me@example.com>'. A price is
--          not expected to hit at all — '<50' and '<$50' do not match, because
--          a digit and '$' are not [a-z].
--
--          If you ever re-add a tag-name branch, end it with PostgreSQL's `\y`
--          word-boundary constraint and NOT with `\b`: in this regex flavour
--          `\b` is the BACKSPACE character (U+0008), so `...(img|...)\b` never
--          matches and the branch silently returns zero rows. Verified:
--            SELECT '<img src=x>' ~* '<\s*(a|img)\b';  -- false
--            SELECT '<img src=x>' ~* '<\s*(a|img)\y';  -- true
-- ===========================================================================
\echo ''
\echo '## D3 — markup / non-https scheme in fields that reach email templates'

  SELECT id, created_at, 'client_requests.description' AS field
    FROM client_requests
   WHERE description ~* '<\s*[a-z]'
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
     AND (calendar_url !~* '^https://'
          OR calendar_url ~ '[<>"''`[:space:]]')
UNION ALL
  SELECT id, created_at, 'mentors.moderation_note'
    FROM mentors
   WHERE moderation_note ~* '<\s*[a-z]'
UNION ALL
  SELECT id, created_at, 'reviews.mentor_review'
    FROM reviews
   WHERE mentor_review ~* '<\s*[a-z]'
UNION ALL
  SELECT id, created_at, 'reviews.platform_review'
    FROM reviews
   WHERE platform_review ~* '<\s*[a-z]'
ORDER BY created_at;


-- ===========================================================================
-- D4 — outstanding review capabilities
--
-- DETECTS  Completed sessions that have no review row yet.
--
-- MEANS    The review link mailed to a mentee is
--          /review?requestId=<client_requests.id>. That UUID is a bearer
--          capability: SubmitReview gates only on Turnstile, the UUID, and
--          "this request is done and has no review yet" — no email, no
--          session, no signature. So every request counted here has a live,
--          unexpired, never-revocable capability outstanding, and the UUID has
--          also been emitted to PostHog as a `distinct_id` (`request:<uuid>`),
--          to OpenTelemetry as a URL path, and to logs on both sides.
--
--          `reviews.client_request_id` is UNIQUE, so a request has at most one
--          review — which is exactly why the capability dies on first use and
--          why the outstanding count equals the number of live capabilities.
--
-- DO       This count SIZES the authorization redesign (audit item H4), it is
--          not itself a repair. A small count (tens) means ship a real
--          capability token and reissue those invitations. A large count (dev
--          showed 138 of 141 = 98%) means a proper expand -> dual-read ->
--          cutover sequence. See outstanding-decisions.md §1.
-- ===========================================================================
\echo ''
\echo '## D4 — completed requests with no review (live review capabilities)'

SELECT
    count(*) FILTER (
        WHERE NOT EXISTS (SELECT 1 FROM reviews r WHERE r.client_request_id = cr.id)
    ) AS outstanding_capabilities,
    count(*) AS done_requests,
    round(
        100.0 * count(*) FILTER (
            WHERE NOT EXISTS (SELECT 1 FROM reviews r WHERE r.client_request_id = cr.id)
        ) / NULLIF(count(*), 0),
        1
    ) AS pct_outstanding
FROM client_requests cr
WHERE cr.status = 'done';


-- ===========================================================================
-- SUMMARY — one line per check. diagnostics.sh reprints from here down.
--
-- The D3 predicate below is a second copy of the D3 query's WHERE clauses.
-- KEEP THE TWO IN SYNC if you edit either.
-- ===========================================================================
\echo ''
\echo '## SUMMARY'

SELECT check_id, finding, next_step
FROM (
    SELECT 1 AS ord,
           'D1 ' AS check_id,
           (SELECT count(*) FROM mentors WHERE sort_order IS NULL)::text
               || ' of ' || (SELECT count(*) FROM mentors)::text
               || ' mentor row(s) have sort_order IS NULL' AS finding,
           'classify with D1b; repair per data-repair.md §D1' AS next_step
    UNION ALL
    SELECT 2,
           'D1b',
           -- COALESCE, not a bare NOT LIKE: airtable_id is NULL for every
           -- natively registered mentor, and NULL NOT LIKE ... is NULL, which
           -- would drop exactly the rows this line is meant to count.
           (SELECT count(*) FROM mentors
             WHERE sort_order IS NULL
               AND COALESCE(airtable_id, '') NOT LIKE 'getmentor:%'
               AND status = 'draft'
               AND activated_at IS NULL)::text
               || ' of those look like stuck registrations',
           'only these may be re-finalized'
    UNION ALL
    SELECT 3,
           'D2a',
           (SELECT count(*) FROM mentors
             WHERE price = 'Free' AND updated_at > created_at)::text
               || ' mentor row(s) at price=Free have been updated since creation',
           'candidates, not proof; see data-repair.md §D2'
    UNION ALL
    SELECT 4,
           'D2b',
           -- NULL included, matching the D2b/D2c detail queries and D2d: see the
           -- D2b comment for why a NULL price is the worse case, not the safe one.
           (SELECT count(*) FROM mentors
             WHERE price IS NULL
                OR price NOT IN ('Free', '$50', '$100', '$150', '$200', 'Negotiable'))::text
               || ' mentor row(s) hold an off-list price',
           'each is one profile save from losing it; fix the form first'
    UNION ALL
    SELECT 5,
           'D2d',
           (SELECT count(*) FROM mentors
             WHERE experience IS NULL
                OR experience NOT IN ('2-5', '5-10', '10+'))::text
               || ' mentor row(s) hold an off-list experience',
           'same uncontrolled-select bug on a second field; fix both'
    UNION ALL
    SELECT 6,
           'D3 ',
           (SELECT count(*) FROM (
                  SELECT 1 FROM client_requests
                   WHERE description ~* '<\s*[a-z]'
               UNION ALL
                  SELECT 1 FROM client_requests WHERE name ~* '<\s*[a-z]'
               UNION ALL
                  SELECT 1 FROM client_requests WHERE preferred_contact ~* '<\s*[a-z]'
               UNION ALL
                  SELECT 1 FROM mentors WHERE name ~* '<\s*[a-z]'
               UNION ALL
                  SELECT 1 FROM mentors WHERE price ~* '<\s*[a-z]'
               UNION ALL
                  SELECT 1 FROM mentors
                   WHERE calendar_url IS NOT NULL AND calendar_url <> ''
                     AND (calendar_url !~* '^https://'
                          OR calendar_url ~ '[<>"''`[:space:]]')
               UNION ALL
                  SELECT 1 FROM mentors WHERE moderation_note ~* '<\s*[a-z]'
               UNION ALL
                  SELECT 1 FROM reviews WHERE mentor_review ~* '<\s*[a-z]'
               UNION ALL
                  SELECT 1 FROM reviews WHERE platform_review ~* '<\s*[a-z]'
           ) h)::text || ' markup / non-https hits',
           'read the D3 rows; a link in a NAME or a javascript: URL is an incident'
    UNION ALL
    SELECT 8,
           'D4 ',
           (SELECT count(*) FROM client_requests cr
             WHERE cr.status = 'done'
               AND NOT EXISTS (SELECT 1 FROM reviews r WHERE r.client_request_id = cr.id))::text
               || ' of ' || (SELECT count(*) FROM client_requests WHERE status = 'done')::text
               || ' completed request(s) have a live review capability',
           'sizes the H4 redesign; see outstanding-decisions.md §1'
) s
ORDER BY ord;

-- Ends the single snapshot opened before D1. Nothing was written, so this
-- COMMIT is just the release of the snapshot and its ACCESS SHARE locks.
COMMIT;

\echo ''
\echo '## END'
