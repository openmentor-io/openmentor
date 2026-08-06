# Runbook: invalidate live tokens after a telemetry leak (P14)

**Incident response, not routine maintenance.** Audit item P14 found that
capability-bearing values reached telemetry: a review `request_id` became a
PostHog *person* (`request:<uuid>`) and an event property, and magic-link /
confirmation tokens could be recorded in Grafana logs and OpenTelemetry span
attributes via the URLs that carry them. The code no longer does this, but
anything already written to PostHog, Loki or Tempo is still there, and every
token that was live during that window should be treated as disclosed to
whoever has read access to those systems.

**Signing out every session no longer needs a `JWT_SECRET` rotation.** Since D58
each identity carries a `session_version` that issued tokens are checked against,
so `UPDATE mentors SET session_version = session_version + 1` (and the same on
`moderators`) invalidates every live cookie with no config change, no deploy and
no restart. Prefer that: rotating the secret was declined precisely because of the
disruption it causes (D56), and it invalidates the *same* set of sessions. Do it in
the same maintenance window as the statements below, so the people affected are
asked to request a fresh link ONCE rather than twice for one incident.

Note the one thing a version bump does not cover: sessions minted before D58 carry
no version claim and are grandfathered for one deploy, so if the incident window
predates that deploy, a `JWT_SECRET` rotation is still the only way to reach them.

Order of operations:

1. Deploy the P14 containment change (so new tokens are not re-leaked).
2. Sign every session out — normally the `session_version` bump above. Only if the
   incident predates the D58 deploy, rotate `JWT_SECRET`: change it in `infra/.env.production`, then
   `./deploy.sh infra`. **Not** `infra/.env.runtime` — P10 deleted that file, and
   each service now receives `JWT_SECRET` through its own `environment:`
   allowlist. See [`secret-rotation.md`](secret-rotation.md) § 6, which also
   explains why this step is scheduled rather than immediate.
3. Run the SQL below.
4. Handle PostHog retention/deletion separately — see "What SQL cannot reach".

## Scope

| What | Where | Effect of invalidating |
|---|---|---|
| Mentor magic-link token | `mentors.login_token` + `login_token_expires_at` | Pending login links stop working; mentor requests a new one from `/mentor/login` |
| Moderator/admin magic-link token | `moderators.login_token` + `login_token_expires_at` | Same, from `/admin/login` |
| Mentor email-confirmation token | `mentors.email_confirmation_token` + `email_confirmation_expires_at` | See the warnings below — do NOT blindly NULL these, and do not rotate expired ones |
| Session cookies (JWT) | `mentors.session_version` / `moderators.session_version` | Bumping the counter invalidates every issued cookie (D58); a `JWT_SECRET` rotation is only needed for sessions minted before that deploy |

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
                                     AND status = 'draft')                     AS confirmation_tokens_draft,
  -- Step 3 only touches these: an expired confirmation token is not a live
  -- capability. NULL expiry counts as expired (the app COALESCEs it to epoch).
  (SELECT count(*) FROM mentors    WHERE email_confirmation_token IS NOT NULL
                                     AND email_confirmation_expires_at > now()) AS confirmation_tokens_still_valid;

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

-- 3. Email-confirmation tokens — ROTATE the LIVE ones, do not clear, and do not
--    touch the expired ones. See the two warnings below.
--
--    SINCE D57 CONFIRMATION TOKENS ARE HASHED AT REST. The column holds
--    encode(sha256(token), 'hex'); the token itself exists only in the email.
--    So this statement generates a plaintext, stores its DIGEST, and keeps the
--    plaintext in the scratch table — step 5 mails it, and nothing else can
--    recover it. Writing a plaintext token into the column (as this runbook used
--    to) would silently undo the hashing for every row it touched.
--
--    Two gen_random_uuid() values (PG13+, CSPRNG-backed) give 64 hex characters;
--    the "mcf_" prefix matches what generateConfirmationToken emits. The expiry
--    is reset to the application's own 24h window (confirmationTokenTTL): a
--    rotated token is only delivered by the resend in step 5, so it has to be
--    valid when that email arrives, not on the old schedule.
--
--    The rotated ids are recorded because step 5 has to mail every one of them
--    and NOTHING in the row identifies them afterwards. The site stays live
--    during this, and both normal paths — registration and ResendConfirmation —
--    set the same 24h window, so any mentor arriving after this statement looks
--    identical to a rotated one. Selecting on a future expiry would mail
--    confirmation links to unrelated people and destroy the incident's scope.
--    This table is operator scratch state, not part of the migration sequence.
--
--    UNLIKE THE PREVIOUS VERSION OF THIS RUNBOOK, THE SCRATCH TABLE HOLDS LIVE
--    TOKENS. That is the price of hashing at rest and it is why step 6 drops the
--    table as soon as the resends have gone out: for the length of the incident
--    response it is a credential store, so keep it in the database (never in a
--    file or a terminal scrollback) and do not leave it behind.
CREATE TABLE IF NOT EXISTS ops_confirmation_rotation (
  mentor_id  uuid PRIMARY KEY,
  status     text        NOT NULL,
  token      text        NOT NULL,
  rotated_at timestamptz NOT NULL DEFAULT now()
);
REVOKE ALL ON ops_confirmation_rotation FROM PUBLIC;

WITH minted AS (
  SELECT id, status,
         'mcf_' || replace(gen_random_uuid()::text, '-', '')
                || replace(gen_random_uuid()::text, '-', '') AS token
    FROM mentors
   WHERE email_confirmation_token IS NOT NULL
     AND email_confirmation_expires_at > now()
), rotated AS (
  UPDATE mentors m
     SET email_confirmation_token = encode(sha256(k.token::bytea), 'hex'),
         email_confirmation_expires_at = now() + interval '24 hours',
         updated_at = now()
    FROM minted k
   WHERE m.id = k.id
  RETURNING m.id, k.status, k.token
)
INSERT INTO ops_confirmation_rotation (mentor_id, status, token)
SELECT id, status, token FROM rotated
RETURNING mentor_id, status;

-- 4. Verify against the recorded set, not against a timestamp predicate: every
--    row it names carries a token the application will accept, and the count
--    matches confirmation_tokens_still_valid from step 0.
--
--    rotated_hashed is the check that matters most: a row whose column still
--    matches the plaintext means the hashing was bypassed.
SELECT count(*)                                                              AS rotated,
       count(*) FILTER (WHERE r.token LIKE 'mcf_%')                          AS rotated_shape_ok,
       count(*) FILTER (WHERE m.email_confirmation_token
                             = encode(sha256(r.token::bytea), 'hex'))        AS rotated_hashed,
       count(*) FILTER (WHERE m.email_confirmation_token = r.token)          AS rotated_plaintext_BAD,
       count(*) FILTER (WHERE m.email_confirmation_expires_at > now())       AS rotated_live,
       count(*) FILTER (WHERE r.status = 'draft')                            AS rotated_draft
  FROM ops_confirmation_rotation r
  JOIN mentors m ON m.id = r.mentor_id;

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

### Warning: do not rotate an ALREADY EXPIRED confirmation token

`GetByConfirmationToken` does not filter on the expiry — it returns the row and
lets the caller choose — which is what makes an expired link recoverable:
`/mentor/confirm` shows "This link has expired" with a Resend button that posts
the *same* expired token to `ResendConfirmation`, which issues a fresh token and
a fresh 24h window (`TestResendConfirmationAcceptsAnExpiredToken` pins this).

Rotating such a row throws that away: the mentor's link stops matching any row,
so it can no longer reach the resend endpoint, and the value that replaced it is
one only the resend in step 5 can deliver. There is nothing to gain by rotating
it either — an expired token opens nothing, because `ConfirmEmail` rejects it on
the expiry check before touching the row. Hence the
`email_confirmation_expires_at > now()` filter in step 3.

### Step 5: deliver the rotated tokens

A rotated token is only useful to the mentor once it is emailed to them. After
COMMIT, re-send the confirmation email for each rotated **draft** mentor. The list
is the one step 3 recorded — read it back, do not re-derive it.

Since D57 the worker cannot read a working link off the row (the row holds a
digest), so the confirm URL goes in the job's PAYLOAD, the same way the
magic-link login URL always has. That means the plaintext token has to travel
from the scratch table into the request:

```bash
# Runs entirely inside the containers, so no token is ever printed to a terminal
# or stored in shell history. WORKER_AUTH_TOKEN comes from the worker's own
# environment. BASE_URL must match the site the mentors use.
docker compose exec -T postgres psql -U openmentor -d openmentor -At -F'|' -c \
  "SELECT mentor_id, token FROM ops_confirmation_rotation WHERE status = 'draft'" \
| docker compose exec -T worker sh -lc '
    while IFS="|" read -r id token; do
      curl -sS -o /dev/null -w "%{http_code} $id\n" -X POST \
        -H "X-Worker-Token: $WORKER_AUTH_TOKEN" -H "Content-Type: application/json" \
        -d "{\"type\":\"mentor_confirm_email\",\"mentor_id\":\"$id\",\"confirm_url\":\"${BASE_URL:-https://openmentor.io}/mentor/confirm?token=$token\"}" \
        "http://localhost:${WORKER_PORT:-8090}/jobs/mentor-confirm-email"
    done'
```

Every line must report `200`. A `409` means the job found no usable link — which
after D57 is what happens if you call it WITHOUT a `confirm_url` against a hashed
row, and is the job deliberately refusing to email a digest as if it were a
token.

Do NOT reconstruct the list from a timestamp predicate. "Every draft mentor whose
confirmation token expires in the future" is NOT the set step 3 touched: the site
stays live, and registration and `ResendConfirmation` both set the same 24h
window, so that query grows with every mentor who arrives afterwards. Mailing
them a confirmation link they did not ask for is the visible damage; losing the
incident's scope is the lasting one. Do not widen it to every draft mentor with a
token either — the untouched expired rows still hold the link their mentor has,
and re-sending it would mail out a link that dies on the expiry check.

If the count is small and re-sending is not worth it, the alternative is to leave
step 3 out entirely and accept that confirmation links issued during the leak
window remain valid — they only grant `draft -> pending` on a profile the holder
already created, which is a much smaller capability than a login token. Decide
explicitly; do not skip it by accident.

### Step 6: drop the scratch table

`ops_confirmation_rotation` holds LIVE confirmation tokens (see step 3) as well as
the list of who was caught in the incident. Drop it as soon as every resend in
step 5 has reported 200 — not "once the incident is closed":

```sql
DROP TABLE ops_confirmation_rotation;
```

## What SQL cannot reach

- **PostHog**: person records keyed `request:<uuid>` and the `request_id`
  property on `review_submitted`, `review_eligibility_checked` and
  `mentee_contact_submitted` events. Delete or expire those from the PostHog
  side; no database statement affects them. The capability is also in
  **autocapture URLs** — `/mentor/requests/<uuid>` reaches `$current_url`,
  `$pathname`, `$referrer`, `$initial_current_url`, `$elements_chain` and, via
  the rrweb Meta event, a session recording's `first_url`. Property-name
  blocklists never covered those; the `before_send` hook now strips UUID path
  segments. See "PostHog cleanup" below.
- **Loki / Tempo**: whatever was written before the containment change ages out
  with the configured retention. If retention is long, consider deleting the
  affected streams.
- **Review capabilities themselves**: `client_requests.id` IS the review
  capability, and it is a primary key referenced by `reviews` — it cannot be
  rotated. Until the review endpoints stop accepting a bare request id as
  authorization (audit item H4), a disclosed request id remains usable by
  whoever holds it. Nothing in this runbook changes that; it is the reason H4
  exists.

## PostHog cleanup

The `before_send` hook only protects events the browser has not sent yet. Three
separate jobs on the PostHog side, in this order:

1. **Add a path-cleaning rule** (Project settings -> Path cleaning rules, or the
   `path-cleaning-rules-update` API). This is the server-side belt and braces:
   it rewrites `$current_url` / `$pathname` **at query time**, so it covers
   events already in flight from browsers running the old bundle, and any client
   we forget. Suggested rule — alias `/mentor/requests/:id`, regex
   `\/mentor\/requests\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`.
   It does NOT rewrite stored data and does not apply to `$referrer`,
   `$elements_chain` or a recording's `first_url`, so it is a mitigation, not a
   deletion.
2. **Delete the `request:<uuid>` persons** (Persons, filter on the prefix). This
   also removes their events.
3. **Scrub the historical event properties.** This is NOT self-serve: PostHog
   exposes no UI or API for rewriting a property on already-ingested events, so
   it needs a **support ticket** asking them to run the scrub. Have the property
   list (`request_id`, `$current_url`, `$pathname`, `$referrer`,
   `$initial_current_url`, `$elements_chain`, replay `first_url`) and the date
   range ready. The self-serve alternatives are deleting the events outright or
   letting retention expire them — decide which, and record the choice.
