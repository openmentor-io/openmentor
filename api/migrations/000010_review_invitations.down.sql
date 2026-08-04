-- Reverses 000010. Dropping the table discards every outstanding review
-- capability, so a rollback must be followed by reissuing invitations for any
-- request whose mentee has not reviewed yet.
DROP TABLE IF EXISTS review_invitations;

ALTER TABLE client_requests DROP COLUMN IF EXISTS review_legacy_link_revoked_at;
