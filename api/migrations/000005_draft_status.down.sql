-- Reverse the draft-status workflow migration.

-- Map draft mentors back to pending before restoring the old constraint.
UPDATE mentors SET status = 'pending' WHERE status = 'draft';

ALTER TABLE mentors DROP CONSTRAINT mentors_status_chk;
ALTER TABLE mentors ADD CONSTRAINT mentors_status_chk
  CHECK (status IN ('pending', 'active', 'inactive', 'declined'));

ALTER TABLE mentors DROP COLUMN photo_style;
ALTER TABLE mentors DROP COLUMN activated_at;
ALTER TABLE mentors DROP COLUMN email_confirmation_expires_at;
ALTER TABLE mentors DROP COLUMN email_confirmation_token;
ALTER TABLE mentors DROP COLUMN moderation_note;
