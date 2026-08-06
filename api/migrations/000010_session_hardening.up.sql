-- phase: expand
-- (additive: new columns with defaults + unique indexes on random tokens;
-- pre-H1 code runs unchanged against this schema, so rollback may cross it)
-- Session hardening (DECISIONS D57, D58). Additive only: two partial unique
-- indexes and one NOT NULL column with a default on each identity table, so an
-- image built before this migration still runs against the schema it creates.

-- 1. One live magic link per identity.
--
-- Both login-token columns hold a SHA-256 hex digest of a 256-bit random token,
-- so a collision between two identities is not a realistic accident. The index
-- is here for the deliberate case: the atomic single-UPDATE consumption in
-- ConsumeLoginToken keys on the token alone, and a unique index is what makes
-- "at most one row can match this token" a schema guarantee rather than a
-- probabilistic one. It also makes the consumption's lookup an index-only match.
--
-- NOT built CONCURRENTLY on purpose. Both tables are small (identities, not
-- events), so the SHARE lock lasts milliseconds, and CONCURRENTLY combined with
-- IF NOT EXISTS has a nastier failure mode than the lock it avoids: a failed
-- concurrent build leaves an INVALID index behind, and the retry then sees a
-- matching name, skips, and reports success over an index that can never be used.
CREATE UNIQUE INDEX IF NOT EXISTS mentors_login_token_uniq
  ON mentors (login_token)
  WHERE login_token IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS moderators_login_token_uniq
  ON moderators (login_token)
  WHERE login_token IS NOT NULL;

-- 2. Session revocation counter.
--
-- Issued JWTs carry the value the row held when the session was minted; the
-- session middleware re-reads it (one primary-key lookup) and refuses a token
-- whose value no longer matches. Bumping it is therefore "log this identity out
-- everywhere", which is what logout now does — clearing the cookie alone left a
-- stolen token valid for the rest of its 24h.
--
-- NOT NULL DEFAULT 1 is a metadata-only change on Postgres 11+, and every
-- existing INSERT keeps working because it never mentions the column. Sessions
-- minted before this migration carry no version claim at all and are grandfathered
-- in by the middleware for one deploy, so nobody is logged out by the rollout.
ALTER TABLE mentors
  ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 1;

ALTER TABLE moderators
  ADD COLUMN IF NOT EXISTS session_version INTEGER NOT NULL DEFAULT 1;
