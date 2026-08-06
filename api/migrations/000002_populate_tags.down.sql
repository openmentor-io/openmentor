-- Remove the seeded tag taxonomy (reverse of 000002_populate_tags.up.sql).
--
-- LOSSY in one direction only, and it matters: tags.id is referenced by
-- mentor_tags with ON DELETE CASCADE, so deleting a seeded tag also deletes
-- every mentor's association with it. There is no record of which mentor held
-- which tag afterwards. Reapplying 000002 brings the tag rows back empty.
--
-- Deleting by id, not by name: 000009 renames several of these tags, so a
-- name-keyed delete would silently miss rows on a database where 000009 has
-- been applied and rolled back. The ids are the ones 000002 pins literally.
--
-- Rolling back this far is only meaningful when going back to 000001, which
-- shipped an empty tags table. Prefer a restore
-- (docs/runbooks/postgres-backup-restore.md) if the mentor associations matter.

DELETE FROM tags
 WHERE id IN (
    '8f3c2a10-7b4d-4c91-a001-000000000001',
    '8f3c2a10-7b4d-4c91-a001-000000000002',
    '8f3c2a10-7b4d-4c91-a001-000000000003',
    '8f3c2a10-7b4d-4c91-a001-000000000004',
    '8f3c2a10-7b4d-4c91-a001-000000000005',
    '8f3c2a10-7b4d-4c91-a001-000000000006',
    '8f3c2a10-7b4d-4c91-a001-000000000007',
    '8f3c2a10-7b4d-4c91-a001-000000000008',
    '8f3c2a10-7b4d-4c91-a001-000000000009',
    '8f3c2a10-7b4d-4c91-a001-00000000000a',
    '8f3c2a10-7b4d-4c91-a001-00000000000b',
    '8f3c2a10-7b4d-4c91-a001-00000000000c',
    '8f3c2a10-7b4d-4c91-a001-00000000000d',
    '8f3c2a10-7b4d-4c91-a001-00000000000e',
    '8f3c2a10-7b4d-4c91-a001-00000000000f',
    '8f3c2a10-7b4d-4c91-a001-000000000010',
    '8f3c2a10-7b4d-4c91-a001-000000000011',
    '8f3c2a10-7b4d-4c91-a001-000000000012',
    '8f3c2a10-7b4d-4c91-a001-000000000013',
    '8f3c2a10-7b4d-4c91-a001-000000000014',
    '8f3c2a10-7b4d-4c91-a001-000000000015',
    '8f3c2a10-7b4d-4c91-a001-000000000016',
    '8f3c2a10-7b4d-4c91-a001-000000000017',
    '8f3c2a10-7b4d-4c91-a001-000000000018',
    '8f3c2a10-7b4d-4c91-a001-000000000019'
 );
