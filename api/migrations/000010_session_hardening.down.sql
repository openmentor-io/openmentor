-- Reverses 000010. Dropping session_version invalidates nothing: the code that
-- reads it treats a missing version claim as "not version-checked", so a rolled
-- back binary simply stops enforcing revocation.

ALTER TABLE moderators DROP COLUMN IF EXISTS session_version;
ALTER TABLE mentors DROP COLUMN IF EXISTS session_version;

DROP INDEX IF EXISTS moderators_login_token_uniq;
DROP INDEX IF EXISTS mentors_login_token_uniq;
