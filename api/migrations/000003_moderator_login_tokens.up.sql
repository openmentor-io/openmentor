-- Add one-time login token support for moderator/admin web authentication

ALTER TABLE moderators
  ADD COLUMN IF NOT EXISTS login_token TEXT,
  ADD COLUMN IF NOT EXISTS login_token_expires_at TIMESTAMPTZ;

-- Email is used for one-time login requests
CREATE UNIQUE INDEX IF NOT EXISTS moderators_email_uniq
  ON moderators (email)
  WHERE email IS NOT NULL;

CREATE INDEX IF NOT EXISTS moderators_login_token_idx
  ON moderators (login_token)
  WHERE login_token IS NOT NULL;
