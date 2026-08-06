-- phase: contract
-- Modernise the mentor tag taxonomy (DECISIONS D30).
--
-- The tag set was inherited from getmentor.dev and reflects the 2018-era
-- Russian-market catalog: no AI/LLM presence at all, an ambiguous "Networking"
-- (computer networks, but reads as professional networking on a mentoring
-- site), and several tags that describe a task rather than expertise.
--
-- Two tags were also offered by the frontend but never seeded, so they were
-- broken in production: "Security" was silently dropped from mentor profiles
-- (GetTagIDByName failed and registration just logged a warning), and "Other"
-- was a filter that could never match anybody.
--
-- Net effect: 25 tags -> 30. Renames keep the row (and therefore every
-- mentor_tags association) intact; only 5 tags are removed, four of them with
-- a remap so affected mentors keep an equivalent tag.
--
-- ORDER MATTERS: rename -> insert new -> remap -> delete. Remaps must run
-- before the deletes, because tags.id is ON DELETE CASCADE into mentor_tags:
-- dropping a tag first would take its mentor associations with it.

-- 1. RENAMES ---------------------------------------------------------------
-- The row keeps its UUID, so mentor associations survive untouched.
UPDATE tags SET name = 'UX/UI Design',              updated_at = NOW() WHERE name = 'UX/UI/Design';
UPDATE tags SET name = 'Mobile',                    updated_at = NOW() WHERE name = 'iOS';
UPDATE tags SET name = 'QA & Test Automation',      updated_at = NOW() WHERE name = 'QA';
UPDATE tags SET name = 'Machine Learning',          updated_at = NOW() WHERE name = 'Data Science/ML';
UPDATE tags SET name = 'Cloud & Infrastructure',    updated_at = NOW() WHERE name = 'Cloud';
UPDATE tags SET name = 'Engineering Management',    updated_at = NOW() WHERE name = 'Team Lead/Management';
UPDATE tags SET name = 'Developer Relations',       updated_at = NOW() WHERE name = 'DevRel';
UPDATE tags SET name = 'People & HR',               updated_at = NOW() WHERE name = 'HR';
UPDATE tags SET name = 'Marketing & Growth',        updated_at = NOW() WHERE name = 'Marketing';
UPDATE tags SET name = 'Entrepreneurship & Startups', updated_at = NOW() WHERE name = 'Entrepreneurship';
UPDATE tags SET name = 'Career Growth',             updated_at = NOW() WHERE name = 'Career';
UPDATE tags SET name = 'Interview Prep',            updated_at = NOW() WHERE name = 'Interview prep';
UPDATE tags SET name = 'Data & Analytics',          updated_at = NOW() WHERE name = 'Analytics';

-- Unchanged (highest-traffic tags deliberately left alone): Backend, Frontend,
-- Product Management, Project Management, System Design, DevOps/SRE, Databases.

-- 2. NEW TAGS --------------------------------------------------------------
-- Deterministic UUIDs continuing the 000002 sequence (…0019 was the last).
INSERT INTO tags (id, name)
VALUES
    ('8f3c2a10-7b4d-4c91-a001-00000000001a', 'Security'),
    ('8f3c2a10-7b4d-4c91-a001-00000000001b', 'AI/LLM Engineering'),
    ('8f3c2a10-7b4d-4c91-a001-00000000001c', 'Data Engineering'),
    ('8f3c2a10-7b4d-4c91-a001-00000000001d', 'Platform Engineering'),
    ('8f3c2a10-7b4d-4c91-a001-00000000001e', 'Tech Lead'),
    ('8f3c2a10-7b4d-4c91-a001-00000000001f', 'Staff+/IC Growth'),
    ('8f3c2a10-7b4d-4c91-a001-000000000020', 'Compensation & Negotiation'),
    ('8f3c2a10-7b4d-4c91-a001-000000000021', 'Relocation & Working Abroad'),
    ('8f3c2a10-7b4d-4c91-a001-000000000022', 'Freelancing & Consulting'),
    ('8f3c2a10-7b4d-4c91-a001-000000000023', 'Technical Writing')
ON CONFLICT (name) DO NOTHING;

-- 3. REMAPS ----------------------------------------------------------------
-- Move mentors off the tags about to be removed. ON CONFLICT DO NOTHING is
-- required: a mentor tagged both iOS and Android (or both Agile and Project
-- Management) would otherwise collide on the (mentor_id, tag_id) primary key.
INSERT INTO mentor_tags (mentor_id, tag_id)
SELECT mt.mentor_id, dst.id
  FROM mentor_tags mt
  JOIN tags src ON src.id = mt.tag_id
  JOIN (VALUES
          ('Android',      'Mobile'),
          ('Networking',   'Cloud & Infrastructure'),
          ('Agile',        'Project Management'),
          ('Content/Copy', 'Technical Writing')
       ) AS remap(src_name, dst_name) ON remap.src_name = src.name
  JOIN tags dst ON dst.name = remap.dst_name
ON CONFLICT (mentor_id, tag_id) DO NOTHING;

-- 4. REMOVALS --------------------------------------------------------------
-- Cascades the remaining mentor_tags rows for these tags (already remapped
-- above, except Code Review which is a task rather than an expertise and had
-- no filter usage in production).
DELETE FROM tags
 WHERE name IN ('Android', 'Networking', 'Agile', 'Content/Copy', 'Code Review');
