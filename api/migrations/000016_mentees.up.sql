-- phase: expand
-- (additive: one new table and one new index; an older image reads neither,
-- so rollback may cross it)
--
-- Mentee identity for the read-only mentee dashboard (D95). A mentee has never
-- had an account: each client_requests row carries the email it was sent from,
-- and that email is the identity. This row exists only to hold what a session
-- needs and a request row cannot — the hashed magic-link token and the
-- session_version revocation counter (D57/D58), the same pair mentors and
-- moderators carry.
--
-- The row is created on the first login request, and only for an email that has
-- at least one client request (MenteeRepository.SetLoginToken), so the table never
-- holds an address that did not already exist in client_requests.
CREATE TABLE IF NOT EXISTS mentees (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email CITEXT NOT NULL,
  -- sha256 hex of the raw token (L1). NEVER the raw token.
  login_token TEXT,
  login_token_expires_at TIMESTAMPTZ,
  session_version INTEGER NOT NULL DEFAULT 1,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT mentees_email_key UNIQUE (email)
);

-- Same guarantee as mentors_login_token_uniq (000010): the consuming UPDATE keys
-- on the token alone, so at most one row may match it.
CREATE UNIQUE INDEX IF NOT EXISTS mentees_login_token_uniq
  ON mentees (login_token)
  WHERE login_token IS NOT NULL;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_trigger WHERE tgname = 'trg_mentees_updated_at') THEN
    CREATE TRIGGER trg_mentees_updated_at
    BEFORE UPDATE ON mentees
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
  END IF;
END $$;

-- The dashboard lists a mentee's requests by email, and the login request checks
-- that one exists. Not CONCURRENTLY for the reason 000010 gives: the table is
-- small, the SHARE lock lasts milliseconds, and a failed concurrent build leaves
-- an INVALID index that IF NOT EXISTS would then skip over.
CREATE INDEX IF NOT EXISTS client_requests_email_idx ON client_requests (email);
