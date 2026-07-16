-- Draft mentor-status workflow: registrations start as 'draft' until the
-- mentor confirms their email; moderators can 'return' a pending profile to
-- draft with a note. Adds the email-confirmation token columns, the
-- moderation note, the first-activation timestamp (hard guard: an
-- ever-activated mentor can never go back to draft) and the auto-detected
-- profile photo style.

-- 1. Extend the status CHECK constraint with 'draft'.
ALTER TABLE mentors DROP CONSTRAINT mentors_status_chk;
ALTER TABLE mentors ADD CONSTRAINT mentors_status_chk
  CHECK (status IN ('draft', 'pending', 'active', 'inactive', 'declined'));

-- 2. New columns.
ALTER TABLE mentors ADD COLUMN moderation_note TEXT;
ALTER TABLE mentors ADD COLUMN email_confirmation_token TEXT UNIQUE;
ALTER TABLE mentors ADD COLUMN email_confirmation_expires_at TIMESTAMPTZ;
ALTER TABLE mentors ADD COLUMN activated_at TIMESTAMPTZ;
ALTER TABLE mentors ADD COLUMN photo_style TEXT NOT NULL DEFAULT 'frame'
  CONSTRAINT mentors_photo_style_chk CHECK (photo_style IN ('hero', 'frame'));

-- 3. Backfill: mentors that are already active were activated in the past.
UPDATE mentors SET activated_at = now() WHERE status = 'active';
