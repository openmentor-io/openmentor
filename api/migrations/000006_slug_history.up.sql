-- Custom usernames (D28): mentors can change their slug. Old slugs live here
-- and 301-redirect to the mentor's current profile. Policy: at most the 2 most
-- recent old slugs per mentor are kept (older rows are deleted and the slug is
-- freed for others to claim).
--
-- slug is the PRIMARY KEY: an old slug redirects to exactly ONE mentor. When a
-- slug is (re)claimed as someone's current username, its history row is
-- deleted so the redirect can never shadow a live profile.
CREATE TABLE mentor_slug_history (
    slug TEXT PRIMARY KEY,
    mentor_id UUID NOT NULL REFERENCES mentors(id) ON DELETE CASCADE,
    -- Who initiated the change that RETIRED this slug. The 14-day change
    -- cooldown only counts mentor-initiated changes (admin fixes don't lock
    -- the mentor out).
    changed_by TEXT NOT NULL CHECK (changed_by IN ('mentor', 'admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX mentor_slug_history_mentor_idx ON mentor_slug_history (mentor_id);
