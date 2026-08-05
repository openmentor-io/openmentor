-- phase: expand
-- (additive: new table + nullable column; pre-H4 code ignores both, so
-- rollback may cross it)
-- H4: move the review capability off client_requests.id.
--
-- Before this migration `client_requests.id` WAS the review capability:
-- POST /api/v1/reviews/<uuid> gated only on Turnstile plus DB eligibility, so
-- anyone who learned that uuid could submit a review attributed to that
-- mentee/mentor pair — and the uuid travelled in an email URL, in logs, in span
-- attributes and in analytics properties. A PostHog audit found 12 of them
-- already sitting in telemetry with a 12-month retention window.
--
-- The replacement is a separate bearer token that is
--   * random    — 32 bytes from crypto/rand, so it cannot be derived from a row id,
--   * hashed    — only sha256(token) is stored, so a database leak yields no
--                 usable capability (the raw value exists solely in the email),
--   * single-use — consumed_at is set by a compare-and-swap in the same
--                 transaction that writes the review,
--   * expiring  — expires_at bounds how long a leaked link stays dangerous.
--
-- EXPAND step of expand -> dual-read -> cutover -> contract. Purely additive:
-- no backfill, no NOT NULL on an existing column, nothing dropped. The legacy
-- request-id path keeps working (dual-read) so invitations already sitting in
-- mentees' inboxes do not break; the cutover that removes it is a follow-up
-- gated on the outstanding-invitation count (D4).
--
-- ===========================================================================
-- MERGE ORDER: 000010 (PR #75) -> 000011 (THIS FILE, PR #77) -> 000012 (#78).
-- ===========================================================================
-- These three migrations are on three branches open at the same time, and
-- `api/pkg/db/migrate.go` runs golang-migrate's `m.Up()`, which applies only
-- versions GREATER THAN the version recorded in `schema_migrations`. So if a
-- HIGHER-numbered sibling merges and deploys first, production records that
-- version and this file is then SILENTLY NEVER APPLIED there — while every
-- fresh database (dev, CI, a staging clone, a DR rebuild) applies it, because
-- those start from version 0. Nothing errors: `migrate` exits 0, the deploy is
-- green, and the divergence only surfaces when the code from this PR queries
-- `review_invitations` against a production schema that has no such table.
--
-- Merging out of order is therefore recoverable ONLY by renumbering before the
-- deploy, or by forcing the version by hand on the VM. Re-check the recorded
-- version at merge time (D61). PR #73 adds the CI guard that fails a PR whose
-- migration version is not greater than the highest version on `origin/main`,
-- so this comment is the record, not the enforcement.

-- A row per issued invitation, not three columns on client_requests, because:
--   * Only the hash is stored, so a resend cannot re-emit the token it already
--     mailed — it has to mint a new one. Appending a row leaves the previously
--     mailed link working (the mistake FinalizeNewMentor documents: never
--     invalidate a credential that is already in someone's inbox); three
--     columns on client_requests would force overwriting it.
--   * client_requests is read on hot paths by SELECTs that list many columns.
--     Keeping the capability in its own relation means the review flow is the
--     only reader, which is also what lets H8 grant it separately.
--   * Consumption is then a single-row UPDATE on a narrow table.
CREATE TABLE IF NOT EXISTS review_invitations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  client_request_id UUID NOT NULL REFERENCES client_requests(id) ON DELETE CASCADE,
  -- sha256 of the raw token, lowercase hex. NEVER the raw token.
  token_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  -- NULL until a submission claims it; set by the compare-and-swap that makes
  -- the token single-use.
  consumed_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  -- Rejects a raw token accidentally stored in place of its digest: a raw
  -- token is 47 characters, a hex sha256 is exactly 64.
  CONSTRAINT review_invitations_token_hash_len_chk CHECK (char_length(token_hash) = 64)
);

-- The lookup index and the uniqueness guarantee are the same object: a token
-- hash identifies at most one invitation, and every read is by hash.
-- Created inline (not CONCURRENTLY) because the table is brand new and empty in
-- this transaction, so there is no row to scan and no lock to contend for.
CREATE UNIQUE INDEX IF NOT EXISTS review_invitations_token_hash_key
  ON review_invitations (token_hash);

-- Supports the operator's "what is outstanding for this request" query and the
-- ON DELETE CASCADE from client_requests.
CREATE INDEX IF NOT EXISTS review_invitations_client_request_id_idx
  ON review_invitations (client_request_id);

-- Per-request kill switch for the LEGACY request-id path, so the 12 uuids that
-- already leaked into telemetry can be neutralised by an operator UPDATE rather
-- than by a deploy. The uuids themselves are deliberately NOT in this file:
-- they are live capabilities and this repository is public, so committing them
-- would be the leak this migration exists to close. See
-- docs/runbooks/audit-2026-08/review-capability-cutover.md.
--
-- Nullable with no default: metadata-only in PostgreSQL 11+, so this does not
-- rewrite the table.
ALTER TABLE client_requests
  ADD COLUMN IF NOT EXISTS review_legacy_link_revoked_at TIMESTAMPTZ;
