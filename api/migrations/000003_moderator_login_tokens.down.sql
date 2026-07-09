DROP INDEX IF EXISTS moderators_login_token_idx;
DROP INDEX IF EXISTS moderators_email_uniq;

ALTER TABLE moderators
  DROP COLUMN IF EXISTS login_token_expires_at,
  DROP COLUMN IF EXISTS login_token;
