-- phase: expand
-- (additive: two nullable columns + two partial indexes; pre-deletion code
-- ignores both, so rollback may cross it)
--
-- Profile deletion (D70). A mentor — or an admin on their behalf — can delete a
-- profile. The row is NOT removed: it is marked, hidden everywhere, and erased
-- for good by the worker's purge job after a retention window.
--
-- WHY A TIMESTAMP AND NOT A 'deleted' STATUS
-- The obvious shape is a sixth member of mentors_status_chk. It was rejected:
-- status answers "where is this profile in the moderation workflow" and every
-- read in the codebase branches on it (login eligibility, the moderation tabs,
-- the visibility toggle, MayHoldMentorSession). Adding 'deleted' there makes
-- deletion a workflow state that every one of those switches has to learn
-- about, and any switch that forgets it treats a deleted profile as live —
-- failing OPEN, which is the wrong direction for this feature.
--
-- deleted_at is orthogonal instead: deletion sets status = 'inactive' (a status
-- every existing branch already handles conservatively — hidden from the
-- catalog, no contact button) AND stamps deleted_at. Code that has not been
-- taught about deletion therefore sees a hidden profile, not a live one, and
-- the deleted_at IS NULL guards added alongside this migration turn "hidden"
-- into "gone". It also records WHEN, which the retention window needs and a
-- status could not carry.
ALTER TABLE mentors ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- Partial: deleted profiles are the rare case, and both readers (the admin
-- "Deleted" tab and the purge job's retention sweep) filter on IS NOT NULL.
-- Indexing only those rows keeps it a few pages instead of one entry per mentor.
CREATE INDEX IF NOT EXISTS mentors_deleted_at_idx
  ON mentors (deleted_at)
  WHERE deleted_at IS NOT NULL;

-- The active-email uniqueness index (000001) is scoped to status = 'active', and
-- deletion moves the row to 'inactive' — so a deleted profile stops reserving
-- its address and the person can register again. That is deliberate: the
-- alternative traps someone who deleted a profile out of their own email for a
-- retention window, and the old row is unreachable by then anyway.

-- Outstanding review invitations belonging to a deleted mentor's requests must
-- stop working the moment the profile goes.
--
-- Not consumed_at: that column means "a review was submitted through this
-- link", and the difference matters to anyone reading the table later — an
-- operator counting outstanding invitations, or the H4 cutover which is gated
-- on exactly that count (D4). Revoked and spent are different facts, so they
-- get different columns.
ALTER TABLE review_invitations ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ;

-- Supports the bulk revoke at deletion time (find the live invitations for a
-- mentor's requests); the token lookups themselves go through the unique hash
-- index and only read revoked_at off the row they already found.
CREATE INDEX IF NOT EXISTS review_invitations_live_idx
  ON review_invitations (client_request_id)
  WHERE consumed_at IS NULL AND revoked_at IS NULL;
