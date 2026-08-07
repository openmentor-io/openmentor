-- Reverses 000013. Dropping deleted_at loses the record of WHICH profiles were
-- deleted and when — the rows themselves survive (that is the point of a soft
-- delete), but they come back as ordinary 'inactive' profiles: hidden from the
-- catalog, and their owners able to log in again. Restoring the marker after
-- the fact is not possible from the database alone.
DROP INDEX IF EXISTS review_invitations_live_idx;
ALTER TABLE review_invitations DROP COLUMN IF EXISTS revoked_at;

DROP INDEX IF EXISTS mentors_deleted_at_idx;
ALTER TABLE mentors DROP COLUMN IF EXISTS deleted_at;
