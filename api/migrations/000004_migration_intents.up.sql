-- Migration intents: getmentor.dev mentors opt in to having their profile
-- migrated (via the public /migrate page). The migration tooling
-- (infra/migration/migrate-mentors.sh --from-intents) consumes pending rows
-- and records the outcome. See DECISIONS D22.

CREATE TABLE IF NOT EXISTS migration_intents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- getmentor.dev mentor slug (e.g. "ivan-petrov-42")
    slug TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'done', 'skipped', 'failed')),
    -- outcome detail written by the migration tooling
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS migration_intents_status_idx ON migration_intents (status);
