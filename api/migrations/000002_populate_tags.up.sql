-- Insert values for tags

INSERT INTO tags (id, name)
VALUES
    ('8f3c2a10-7b4d-4c91-a001-000000000001', 'Backend'),
    ('8f3c2a10-7b4d-4c91-a001-000000000002', 'Frontend'),
    ('8f3c2a10-7b4d-4c91-a001-000000000003', 'UX/UI/Design'),
    ('8f3c2a10-7b4d-4c91-a001-000000000004', 'iOS'),
    ('8f3c2a10-7b4d-4c91-a001-000000000005', 'Android'),
    ('8f3c2a10-7b4d-4c91-a001-000000000006', 'QA'),
    ('8f3c2a10-7b4d-4c91-a001-000000000007', 'Data Science/ML'),
    ('8f3c2a10-7b4d-4c91-a001-000000000008', 'Networking'),
    ('8f3c2a10-7b4d-4c91-a001-000000000009', 'Cloud'),
    ('8f3c2a10-7b4d-4c91-a001-00000000000a', 'Team Lead/Management'),
    ('8f3c2a10-7b4d-4c91-a001-00000000000b', 'Project Management'),
    ('8f3c2a10-7b4d-4c91-a001-00000000000c', 'Product Management'),
    ('8f3c2a10-7b4d-4c91-a001-00000000000d', 'DevRel'),
    ('8f3c2a10-7b4d-4c91-a001-00000000000e', 'HR'),
    ('8f3c2a10-7b4d-4c91-a001-00000000000f', 'Agile'),
    ('8f3c2a10-7b4d-4c91-a001-000000000010', 'Content/Copy'),
    ('8f3c2a10-7b4d-4c91-a001-000000000011', 'Marketing'),
    ('8f3c2a10-7b4d-4c91-a001-000000000012', 'DevOps/SRE'),
    ('8f3c2a10-7b4d-4c91-a001-000000000013', 'Databases'),
    ('8f3c2a10-7b4d-4c91-a001-000000000014', 'Entrepreneurship'),
    ('8f3c2a10-7b4d-4c91-a001-000000000015', 'Career'),
    ('8f3c2a10-7b4d-4c91-a001-000000000016', 'Interview prep'),
    ('8f3c2a10-7b4d-4c91-a001-000000000017', 'Code Review'),
    ('8f3c2a10-7b4d-4c91-a001-000000000018', 'System Design'),
    ('8f3c2a10-7b4d-4c91-a001-000000000019', 'Analytics')
ON CONFLICT (name)
    DO UPDATE SET updated_at = EXCLUDED.updated_at


