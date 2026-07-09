-- Rollback initial schema migration
-- Drop tables in reverse dependency order

-- Drop triggers first
DROP TRIGGER IF EXISTS trg_moderators_updated_at ON moderators;
DROP TRIGGER IF EXISTS trg_reviews_updated_at ON reviews;
DROP TRIGGER IF EXISTS trg_client_requests_updated_at ON client_requests;
DROP TRIGGER IF EXISTS trg_tags_updated_at ON tags;
DROP TRIGGER IF EXISTS trg_mentors_updated_at ON mentors;

-- Drop trigger function
DROP FUNCTION IF EXISTS set_updated_at();

-- Drop tables in reverse dependency order
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS mentor_tags;
DROP TABLE IF EXISTS client_requests;
DROP TABLE IF EXISTS moderators;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS mentors;

-- Drop extensions
DROP EXTENSION IF EXISTS citext;
DROP EXTENSION IF EXISTS pgcrypto;
