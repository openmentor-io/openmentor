# Runbook: invalidate live tokens after a telemetry leak (P14)

**Incident response, not routine maintenance.** Audit item P14 found that
capability-bearing values reached telemetry: a review `request_id` became a
PostHog *person* (`request:<uuid>`) and an event property, and magic-link /
confirmation tokens could be recorded in Grafana logs and OpenTelemetry span
attributes via the URLs that carry them. The code no longer does this, but
anything already written to PostHog, Loki or Tempo is still there, and every
token that was live during that window should be treated as disclosed to
whoever has read access to those systems.

**Pair this with the `JWT_SECRET` rotation.** Rotating `JWT_SECRET` invalidates
every issued session cookie; the statements below invalidate every one-time
login and confirmation token. Doing them in one maintenance window means users
are signed out and asked to request a fresh link ONCE. Doing them separately
disrupts the same people twice, for the same incident.

Order of operations:

1. Deploy the P14 containment change (so new tokens are not re-leaked).
2. Rotate `JWT_SECRET` in `infra/.env.runtime`, restart api + worker.
3. Run the SQL below.
4. Handle PostHog retention/deletion separately — see "What SQL cannot reach".

## Scope

| What | Where | Effect of invalidating |
|---|---|---|
| Mentor magic-link token | `mentors.login_token` + `login_token_expires_at` | Pending login links stop working; mentor requests a new one from `/mentor/login` |
| Moderator/admin magic-link token | `moderators.login_token` + `login_token_expires_at` | Same, from `/admin/login` |
| Mentor email-confirmation token | `mentors.email_confirmation_token` + `email_confirmation_expires_at` | See the warning below — do NOT blindly NULL these |
| Session cookies (JWT) | not in the database | Invalidated by the `JWT_SECRET` rotation, not by SQL |

## Procedure

Run as the application role against the production database (`docker compose exec
-T postgres psql -U openmentor -d openmentor`). Everything is in one
transaction so a mistake can be rolled back before COMMIT.

```sql
BEGIN;

-- 0. Record what is about to be invalidated, so the blast radius is known
--    before anything changes. Emails are counted, not listed.
SELECT
  (SELECT count(*) FROM mentors    WHERE login_token IS NOT NULL)              AS mentor_login_tokens,
  (SELECT count(*) FROM mentors    WHERE login_token IS NOT NULL
                                     AND login_token_expires_at > now())       AS mentor_login_tokens_still_valid,
  (SELECT count(*) FROM moderators WHERE login_token IS NOT NULL)              AS moderator_login_tokens,
  (SELECT count(*) FROM moderators WHERE login_token IS NOT NULL
                                     AND login_token_expires_at > now())       AS moderator_login_tokens_still_valid,
  (SELECT count(*) FROM mentors    WHERE email_confirmation_token IS NOT NULL) AS confirmation_tokens,
  (SELECT count(*) FROM mentors    WHERE email_confirmation_token IS NOT NULL
                                     AND status = 'draft')                     AS confirmation_tokens_draft;

-- 1. Mentor magic-link tokens. Clearing both columns is exactly what
--    ClearLoginToken does after a successful login, so this is a state the
--    application already handles: the link 404s as "invalid or expired" and the
--    mentor requests a new one.
UPDATE mentors
   SET login_token = NULL,
       login_token_expires_at = NULL,
       updated_at = now()
 WHERE login_token IS NOT NULL;

-- 2. Moderator/admin magic-link tokens (separate table, same columns).
UPDATE moderators
   SET login_token = NULL,
       login_token_expires_at = NULL,
       updated_at = now()
 WHERE login_token IS NOT NULL;

-- 3. Email-confirmation tokens — ROTATE, do not clear. See the warning below.
--    Two gen_random_uuid() values (PG13+, CSPRNG-backed) give 64 hex characters;
--    the "mcf_" prefix matches what generateConfirmationToken emits. Expiry is
--    left untouched so a rotated token dies on its original schedule.
UPDATE mentors
   SET email_confirmation_token = 'mcf_' || replace(gen_random_uuid()::text, '-', '')
                                         || replace(gen_random_uuid()::text, '-', ''),
       updated_at = now()
 WHERE email_confirmation_token IS NOT NULL;

-- 4. Verify: no old value survives, and every draft mentor still HAS a token.
SELECT count(*) AS confirmation_tokens_after,
       count(*) FILTER (WHERE email_confirmation_token LIKE 'mcf_%') AS rotated_ok
  FROM mentors
 WHERE email_confirmation_token IS NOT NULL;

COMMIT;
```

### Warning: clearing a confirmation token strands the mentor

`POST /api/v1/mentors/confirm/resend` looks the mentor up **by the token in the
link they already have** (`GetByConfirmationToken`). There is no
"resend by email address" endpoint. So:

- `email_confirmation_token = NULL` on a `draft` mentor removes their only path
  to confirm AND their only path to request a resend. They would have to
  re-register, and the unique index on `mentors.email` will reject that.
- Rotating the token (step 3) has the same security effect — the leaked value no
  longer opens anything — but keeps the row in a state the application
  understands.

A rotated token is only useful to the mentor once it is emailed to them. After
COMMIT, re-send the confirmation email for each affected draft mentor by
invoking the worker job that reads the fresh token off the row:

```bash
# One call per mentor id, from inside the worker container (curl is present in
# the runtime image). WORKER_AUTH_TOKEN is read from the container's own
# environment — never paste it on the command line, where it lands in shell
# history and in the audit log.
docker compose exec -T worker sh -lc \
  'curl -sS -X POST -H "X-Worker-Token: $WORKER_AUTH_TOKEN" \
     "http://localhost:${WORKER_PORT:-8090}/jobs/mentor-confirm-email?mentorId=<MENTOR_ID>"'
```

Get the ids with:

```sql
SELECT id FROM mentors WHERE status = 'draft' AND email_confirmation_token IS NOT NULL;
```

If the draft count is small and re-sending is not worth it, the alternative is
to leave step 3 out entirely and accept that confirmation links issued during
the leak window remain valid — they only grant `draft -> pending` on a profile
the holder already created, which is a much smaller capability than a login
token. Decide explicitly; do not skip it by accident.

## What SQL cannot reach

- **PostHog**: person records keyed `request:<uuid>` and the `request_id`
  property on `review_submitted`, `review_eligibility_checked` and
  `mentee_contact_submitted` events. Delete or expire those from the PostHog
  side; no database statement affects them.
- **Loki / Tempo**: whatever was written before the containment change ages out
  with the configured retention. If retention is long, consider deleting the
  affected streams.
- **Review capabilities themselves**: `client_requests.id` IS the review
  capability, and it is a primary key referenced by `reviews` — it cannot be
  rotated. Until the review endpoints stop accepting a bare request id as
  authorization (audit item H4), a disclosed request id remains usable by
  whoever holds it. Nothing in this runbook changes that; it is the reason H4
  exists.
