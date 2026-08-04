-- phase: expand
-- Cooldown timestamp for mentor-initiated username (slug) changes (D29).
--
-- The 14-day cooldown previously read the newest changed_by='mentor' row from
-- mentor_slug_history, but that history is trimmed to 2 hops — so two admin
-- renames after a mentor rename could delete the cooldown-bearing row and let
-- the mentor rename again early. Store the cooldown timestamp on the mentor
-- row instead, independent of redirect retention. NULL = never self-renamed.
ALTER TABLE mentors ADD COLUMN slug_changed_at TIMESTAMPTZ;

-- Backfill from any surviving mentor-initiated history so existing cooldowns
-- carry over.
UPDATE mentors m
   SET slug_changed_at = h.last
  FROM (
        SELECT mentor_id, MAX(created_at) AS last
          FROM mentor_slug_history
         WHERE changed_by = 'mentor'
         GROUP BY mentor_id
       ) h
 WHERE h.mentor_id = m.id;
