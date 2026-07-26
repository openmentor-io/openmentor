ALTER TABLE mentors DROP CONSTRAINT IF EXISTS mentors_legacy_sessions_count_chk;
ALTER TABLE mentors DROP COLUMN IF EXISTS legacy_sessions_count;
