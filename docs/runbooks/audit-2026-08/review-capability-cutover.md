# Runbook: retire the legacy review link (H4 cutover)

**Operator-facing.** The H4 change replaced review-by-primary-key authorization
with a separate single-use token, but it deliberately did **not** remove the old
path. This file is the two follow-up jobs that need production data:

1. **§1 — neutralise the request ids that already leaked into telemetry.**
   Do this soon after the H4 deploy. It is the only remedy available for them.
2. **§2 — the cutover**: stop accepting `?request_id=` review links at all. This
   is gated on a metric being zero, not on a date.

Item ids (`H4`, `P14`, `D4`) are the remediation plan's
([`../../audit/2026-08-remediation-plan.md`](../../audit/2026-08-remediation-plan.md)).

---

## What H4 changed

| | Before | After |
|---|---|---|
| Capability | `client_requests.id` (a primary key, in the email URL's query string) | 32 random bytes, `rvw_` + base64url |
| At rest | the row itself | `review_invitations.token_hash` — sha256 only |
| Delivery | `…/reviews/new?request_id=<uuid>` | `…/reviews/new#review_token=<token>` (a **fragment**: never sent to a server) |
| Transport to the API | URL path | POST body |
| Reuse | possible until a review existed; the loser of a race got a 500 | single-use by compare-and-swap on `consumed_at`; the loser gets a typed 409 |
| Expiry | never | 30 days (`reviewtoken.TTL`) |
| Scope | whoever learned the uuid | whoever received the email |

Both paths are live during the dual-read window. The switch is
`REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED` (default `true`, backend only).

---

## 1. The request ids that already leaked

A PostHog audit found **12 distinct `client_requests.id` values already in
telemetry**: 12 phantom person records, 55 events across 7 event names, the uuid
in `$current_url` for 93 events across 21 sessions, and in 15 session replays'
`first_url`. Event retention is 12 months; replay retention is 30 days;
historical event-property scrubbing is **not** self-serve on the current plan.

**So those 12 capabilities cannot be un-leaked.** Anyone with PostHog read access
to that project can still read them for up to a year. Deleting the phantom
persons (see [`../telemetry-leak-token-invalidation.md`](../telemetry-leak-token-invalidation.md))
removes the person records but not the `$current_url` property or the replay
`first_url`.

The only real remedy is to make the leaked value stop authorizing anything, which
is what `client_requests.review_legacy_link_revoked_at` is for.

### Why the ids are not in this repository or in a migration

They are live capabilities and this repository is public. Committing them — in a
migration, a test fixture or this file — would publish the leak instead of
closing it. The list stays in PostHog and in the operator's hands; the migration
ships the *mechanism*, and you supply the values.

### Procedure

**Step 1 — get the list out of PostHog.** In the project's SQL editor:

```sql
-- Every request uuid that reached $current_url, across the retention window.
SELECT DISTINCT
  extract(properties.$current_url, 'request_id=([0-9a-f-]{36})') AS request_id
FROM events
WHERE properties.$current_url LIKE '%request_id=%'
  AND timestamp > now() - INTERVAL 400 DAY
```

Also check the phantom persons, whose `distinct_id` was `request:<uuid>` before
`RequestDistinctID` was deleted:

```sql
SELECT DISTINCT distinct_id FROM events
WHERE distinct_id LIKE 'request:%' AND timestamp > now() - INTERVAL 400 DAY
```

Union the two, strip the `request:` prefix, and keep the list in a scratch file
you delete afterwards. **Do not paste it into a ticket, a chat message or a
commit.**

**Step 2 — revoke.** As the application role
(`docker compose exec -T postgres psql -U openmentor -d openmentor`):

```sql
BEGIN;

-- Substitute the real uuids. The count is the check: it must equal the number
-- of ids you pasted, minus any that are no longer in the table.
UPDATE client_requests
   SET review_legacy_link_revoked_at = now()
 WHERE id = ANY(ARRAY[
         '00000000-0000-0000-0000-000000000000'  -- replace
       ]::uuid[])
   AND review_legacy_link_revoked_at IS NULL
RETURNING id, status,
          EXISTS(SELECT 1 FROM reviews r WHERE r.client_request_id = id) AS already_reviewed;

COMMIT;
```

After this, `GET /api/v1/reviews/<that uuid>/check` answers exactly like an
unknown request — `canSubmit: false`, **no mentor name** — and
`POST /api/v1/reviews/<that uuid>` refuses. The revocation targets the legacy
link only; a token issued for the same request still works. (Pinned by
`TestLegacyLinkRevocationHidesTheRequest`.)

**Step 3 — reissue, but only where it matters.** A row whose `already_reviewed`
came back `true` needs nothing: the review exists, the link was dead anyway.
For the rest, mint a fresh capability and mail it by replaying the notification:

```bash
curl -fsS -X POST -H "X-Worker-Token: $WORKER_AUTH_TOKEN" \
  "http://openmentor-worker:8090/jobs/request-process-finished?requestId=<uuid>"
```

This is safe to replay for a request in status `done`: it appends a new
invitation row and sends one `session-complete` email. It does **not** invalidate
any invitation already outstanding — see `CreateReviewInvitation` for why append
rather than update. Expect the mentee to receive a second "got 60 seconds?" mail;
that is the cost of the leak.

> `requestId` is in the query string of that internal job URL. It is
> worker-internal (behind `X-Worker-Token`, not routed publicly) and the P14
> containment redacts it out of the worker's own logs and spans. Nothing to fix
> here, but do not paste that command into a shared terminal recording.

---

## 2. The cutover

Setting `REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED=false` makes both legacy
endpoints answer `410 Gone`. It is **irreversible from the mentee's side**: a
mentee holding only an old link has no way to get a new one except by asking.

### What must be true before flipping it

1. **The metric has been zero long enough.** Confirm in Grafana:

   ```promql
   sum(increase(openmentor_review_legacy_link_uses_total{outcome="accepted"}[30d]))
   ```

   This must be **0 over a window longer than `reviewtoken.TTL` (30 days)**. Any
   non-zero value is a real mentee using a real old link, and flipping the switch
   breaks them. The equivalent PostHog check is
   `review_submitted` / `review_eligibility_checked` filtered to
   `link_type = "legacy_request_id"`.

2. **No `session-complete` email older than the H4 deploy is still actionable.**

   ```sql
   -- Completed requests with no review and no live token: these mentees hold
   -- ONLY a legacy link. The cutover silently ends their ability to review.
   SELECT count(*) AS legacy_only_outstanding
     FROM client_requests cr
    WHERE cr.status = 'done'
      AND cr.review_legacy_link_revoked_at IS NULL
      AND NOT EXISTS (SELECT 1 FROM reviews r WHERE r.client_request_id = cr.id)
      AND NOT EXISTS (SELECT 1 FROM review_invitations i
                       WHERE i.client_request_id = cr.id
                         AND i.consumed_at IS NULL
                         AND i.expires_at > now());
   ```

   This is the production answer to diagnostic **`D4`**, which could not be run
   when H4 was written. Dev showed 138 of 141 completed requests (98%) with no
   review — if production looks like that, this number is large and the cutover
   is premature. Either reissue for those rows (§1 step 3, which is safe to run
   in bulk) or accept losing those reviews, explicitly.

3. **The 30-day window since the last `session-complete` send has elapsed**, so
   every outstanding *token* has expired too and nothing is left to lose.

### Flipping it

```bash
# infra/.env.production
REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED=false
```

then `cd infra && ./deploy.sh infra`. Backend only — the worker has no legacy
path, and `migrate` does not read this.

Verify:

```bash
# Expect 410 with the "no longer supported" body.
curl -si -X POST http://openmentor-backend:8081/api/v1/reviews/<any-uuid> \
  -H 'Content-Type: application/json' -d '{}' | head -1
```

Watch `openmentor_review_legacy_link_uses_total{outcome="refused"}`. A rising
count after the cutover means somebody's link just broke; each one is a
`hello@openmentor.io` conversation, not a rollback.

---

## 3. Contract (the code deletion), afterwards

Only once §2 has been live and quiet for a release cycle. It is a code change,
not an operator action, and it is deliberately a separate PR:

- delete `ReviewService.CheckReview` / `SubmitReview`, `CheckCanSubmitReview` /
  `SubmitReviewForRequest`, and the two legacy routes in `api/cmd/api/main.go`;
- drop the `requestId` branches from `web/src/pages/api/reviews/*.ts` and the
  `request_id` handling in `web/src/pages/reviews/new.tsx`;
- drop `REVIEW_LEGACY_REQUEST_ID_LINKS_ENABLED` from `api/config/config.go` and
  the four infra files that carry it;
- keep `client_requests.review_legacy_link_revoked_at` until the column is
  provably unread, and drop it in its own migration. Migrations in this repo are
  additive-only while the audit remediation is in flight.

Leave `openmentor_review_legacy_link_uses_total` in place for one more release so
the dashboards do not lose their history mid-panel.
