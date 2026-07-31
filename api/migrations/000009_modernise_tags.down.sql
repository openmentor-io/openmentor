-- Revert the tag taxonomy modernisation (D30).
--
-- PARTIALLY LOSSY — read before rolling back. The renames reverse cleanly and
-- the added tags are removed, but two things cannot be restored:
--
--   1. Mentor associations for the deleted tags (Android, Networking, Agile,
--      Content/Copy, Code Review). The up-migration remapped mentors onto
--      replacement tags and then cascaded the originals away; there is no
--      record of which mentor held which original tag. The tag rows come back
--      empty.
--   2. Associations mentors gained on the new tags (Security, AI/LLM
--      Engineering, …) are cascaded away with them.
--
-- Recovering those requires a database restore, not this file.

-- 1. Remove the added tags (cascades their mentor_tags rows).
DELETE FROM tags
 WHERE name IN (
    'Security',
    'AI/LLM Engineering',
    'Data Engineering',
    'Platform Engineering',
    'Tech Lead',
    'Staff+/IC Growth',
    'Compensation & Negotiation',
    'Relocation & Working Abroad',
    'Freelancing & Consulting',
    'Technical Writing'
 );

-- 2. Restore the removed tags, with their original UUIDs but no associations.
INSERT INTO tags (id, name)
VALUES
    ('8f3c2a10-7b4d-4c91-a001-000000000005', 'Android'),
    ('8f3c2a10-7b4d-4c91-a001-000000000008', 'Networking'),
    ('8f3c2a10-7b4d-4c91-a001-00000000000f', 'Agile'),
    ('8f3c2a10-7b4d-4c91-a001-000000000010', 'Content/Copy'),
    ('8f3c2a10-7b4d-4c91-a001-000000000017', 'Code Review')
ON CONFLICT (name) DO NOTHING;

-- 3. Reverse the renames.
UPDATE tags SET name = 'UX/UI/Design',         updated_at = NOW() WHERE name = 'UX/UI Design';
UPDATE tags SET name = 'iOS',                  updated_at = NOW() WHERE name = 'Mobile';
UPDATE tags SET name = 'QA',                   updated_at = NOW() WHERE name = 'QA & Test Automation';
UPDATE tags SET name = 'Data Science/ML',      updated_at = NOW() WHERE name = 'Machine Learning';
UPDATE tags SET name = 'Cloud',                updated_at = NOW() WHERE name = 'Cloud & Infrastructure';
UPDATE tags SET name = 'Team Lead/Management', updated_at = NOW() WHERE name = 'Engineering Management';
UPDATE tags SET name = 'DevRel',               updated_at = NOW() WHERE name = 'Developer Relations';
UPDATE tags SET name = 'HR',                   updated_at = NOW() WHERE name = 'People & HR';
UPDATE tags SET name = 'Marketing',            updated_at = NOW() WHERE name = 'Marketing & Growth';
UPDATE tags SET name = 'Entrepreneurship',     updated_at = NOW() WHERE name = 'Entrepreneurship & Startups';
UPDATE tags SET name = 'Career',               updated_at = NOW() WHERE name = 'Career Growth';
UPDATE tags SET name = 'Interview prep',       updated_at = NOW() WHERE name = 'Interview Prep';
UPDATE tags SET name = 'Analytics',            updated_at = NOW() WHERE name = 'Data & Analytics';
