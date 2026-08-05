-- phase: expand
-- Retroactive session history carried over from getmentor.dev (DECISIONS D28).
--
-- Mentors migrated per D22 arrive with an empty client_requests history even
-- though they ran sessions on getmentor.dev for years. This column stores that
-- prior count as a single number; the mentor read queries add it to the
-- COUNT(*) of 'done' client_requests so the public session count is the
-- mentor's full history. Native rows are never synthesized — client_requests
-- stays the record of sessions that happened on OpenMentor.
--
-- Provenance lives in mentors.airtable_id ('getmentor:<old legacy_id>', D22):
-- a non-zero value here always belongs to a row carrying that marker.

ALTER TABLE mentors
  ADD COLUMN IF NOT EXISTS legacy_sessions_count INTEGER NOT NULL DEFAULT 0;

ALTER TABLE mentors
  DROP CONSTRAINT IF EXISTS mentors_legacy_sessions_count_chk;

ALTER TABLE mentors
  ADD CONSTRAINT mentors_legacy_sessions_count_chk CHECK (legacy_sessions_count >= 0);

COMMENT ON COLUMN mentors.legacy_sessions_count IS
  'Completed sessions carried over from getmentor.dev at migration time (D28). Added to the done client_requests count for display; 0 for mentors who registered on OpenMentor.';
