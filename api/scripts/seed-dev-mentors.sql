-- Dev-only sample data: 12 catalog mentors (matching the redesign mockup
-- personas) + one draft-with-reviewer-note and one pending fixture for the
-- moderation/dashboard flows. Idempotent via ON CONFLICT (slug) DO NOTHING.
--
-- Photos: web/public/sample-images/<slug>/{full,large,small} (gitignored;
-- see docs — served by the dev frontend, NEXT_PUBLIC_CDN_ENDPOINT=
-- localhost:3000/sample-images). Mentors without a photo exercise the
-- initials card. photo_style exercises hero vs frame treatments.
--
-- Fixed login tokens (dashboard testing, far expiry):
--   draft mentor:  /mentor/login/verify?token=dev-login-draft
--   active mentor: /mentor/login/verify?token=dev-login-active
--
-- Usage (dev compose stack):
--   docker exec -i openmentor-postgres-dev psql -U openmentor openmentor \
--     < api/scripts/seed-dev-mentors.sql

BEGIN;

INSERT INTO mentors (legacy_id, slug, name, job_title, workplace, about, details, competencies,
                     experience, price, status, email, preferred_contact, privacy, sort_order,
                     photo_style, created_at, activated_at, login_token, login_token_expires_at,
                     moderation_note)
VALUES
  (1001, 'jonas-weber-1001', 'Jonas Weber', 'Engineering Manager', 'Zalando',
   '<p>I lead two platform teams at Zalando and spent the previous decade shipping backend systems at scale. I moved from senior engineer to manager the hard way — without a mentor — and I''d like to make that transition easier for you.</p><p>Expect direct, structured conversations with homework in between.</p>',
   '<ul><li><p>preparing for your first engineering-management role;</p></li><li><p>running 1:1s, feedback and delegation that actually works;</p></li><li><p>navigating promotion cases and org politics.</p></li></ul>',
   'Engineering management, team topologies, feedback, delegation, backend architecture',
   '5-10', '$50', 'active', 'jonas.weber@example.com', 'Telegram: @jonasweber', false, 10,
   'frame', now() - interval '200 days', now() - interval '199 days', 'dev-login-active', now() + interval '365 days', NULL),

  (1002, 'amara-okafor-1002', 'Amara Okafor', 'Staff Engineer', 'Spotify',
   '<p>Staff engineer on Spotify''s core infrastructure. I care about technical strategy, design docs that persuade, and helping senior engineers find their staff+ path.</p>',
   '<ul><li><p>growing from senior to staff;</p></li><li><p>writing design docs and RFCs;</p></li><li><p>system design interview prep at the senior level.</p></li></ul>',
   'Distributed systems, technical strategy, design reviews, staff+ career paths',
   '10+', 'Free', 'active', 'amara.okafor@example.com', NULL, false, 20,
   'hero', now() - interval '3 days', now() - interval '2 days', NULL, NULL, NULL),

  (1003, 'ahmed-hassan-1003', 'Ahmed Hassan', 'Mobile Engineer', 'Careem',
   '<p>Eight years of shipping iOS and Android apps in high-growth startups across MENA. I mentor engineers who want to go deep on mobile or break into the field from adjacent stacks.</p>',
   '<ul><li><p>mobile architecture (both platforms);</p></li><li><p>portfolio and CV reviews for mobile roles;</p></li><li><p>moving from web to mobile development.</p></li></ul>',
   'iOS, Android, Swift, Kotlin, mobile architecture, app performance',
   '5-10', '$30', 'active', 'ahmed.hassan@example.com', NULL, false, 30,
   'frame', now() - interval '120 days', now() - interval '119 days', NULL, NULL, NULL),

  (1004, 'ingrid-johansson-1004', 'Ingrid Johansson', 'UX Researcher', 'Klarna',
   '<p>I run discovery research at Klarna and previously built the research practice at two Series-B startups. Happy to help designers add research rigor and researchers find their footing in product orgs.</p>',
   '<ul><li><p>planning and running user studies;</p></li><li><p>making research land with product teams;</p></li><li><p>breaking into UX research.</p></li></ul>',
   'User research, usability testing, research ops, discovery, service design',
   '10+', 'Negotiable', 'active', 'ingrid.johansson@example.com', NULL, false, 40,
   'frame', now() - interval '5 days', now() - interval '4 days', NULL, NULL, NULL),

  (1005, 'sean-obrien-1005', 'Sean O''Brien', 'Principal Platform Infrastructure Architect', 'Stripe',
   '<p>I design the platforms other engineers build on. Two decades across infra, SRE and developer experience — currently principal architect at Stripe. I mentor senior ICs wrestling with scale, reliability and influence.</p>',
   '<ul><li><p>platform and infrastructure architecture reviews;</p></li><li><p>reliability culture and incident learning;</p></li><li><p>influencing without authority at principal level.</p></li></ul>',
   'Platform engineering, Kubernetes, reliability, architecture reviews, technical influence',
   '10+', '$150', 'active', 'sean.obrien@example.com', NULL, false, 50,
   'frame', now() - interval '300 days', now() - interval '299 days', NULL, NULL, NULL),

  (1006, 'priya-sharma-1006', 'Priya Sharma', 'Product Designer', 'Figma',
   '<p>Product designer at Figma, previously design systems lead at two scale-ups. I mentor designers on craft, portfolios and the systems side of design.</p>',
   '<ul><li><p>portfolio reviews that get interviews;</p></li><li><p>design systems from scratch;</p></li><li><p>working effectively with engineers.</p></li></ul>',
   'Product design, design systems, prototyping, portfolio coaching',
   '5-10', 'Free', 'active', 'priya.sharma@example.com', NULL, false, 60,
   'frame', now() - interval '90 days', now() - interval '89 days', NULL, NULL, NULL),

  (1007, 'yuki-tanaka-1007', 'Yuki Tanaka', 'Site Reliability Engineer', 'Mercari',
   '<p>SRE at Mercari focused on observability and on-call health. I like helping ops-minded engineers modernize their practice and their careers.</p>',
   '<ul><li><p>SLOs, alerting and observability stacks;</p></li><li><p>moving from sysadmin to SRE;</p></li><li><p>surviving and improving on-call.</p></li></ul>',
   'SRE, observability, incident response, Prometheus, on-call culture',
   '10+', '$150', 'active', 'yuki.tanaka@example.com', NULL, false, 70,
   'frame', now() - interval '150 days', now() - interval '149 days', NULL, NULL, NULL),

  (1008, 'elif-kaya-1008', 'Elif Kaya', 'Product Manager', 'Getir',
   '<p>PM at Getir through hypergrowth and consolidation. I mentor new PMs and engineers considering the switch to product.</p>',
   '<ul><li><p>breaking into product management;</p></li><li><p>discovery, prioritization and stakeholder management;</p></li><li><p>surviving your first 90 days as a PM.</p></li></ul>',
   'Product management, discovery, roadmaps, stakeholder management, agile',
   '5-10', 'Negotiable', 'active', 'elif.kaya@example.com', NULL, false, 80,
   'frame', now() - interval '60 days', now() - interval '59 days', NULL, NULL, NULL),

  (1009, 'daria-kovalenko-1009', 'Daria Kovalenko', 'Frontend Lead', 'N26',
   '<p>Frontend lead at N26. I review a lot of code and a lot of careers — happy to do both for you.</p>',
   '<ul><li><p>modern React/TypeScript architecture;</p></li><li><p>code review culture;</p></li><li><p>senior frontend interview prep.</p></li></ul>',
   'React, TypeScript, frontend architecture, code review, web performance',
   '5-10', '$50', 'active', 'daria.kovalenko@example.com', NULL, false, 90,
   'frame', now() - interval '45 days', now() - interval '44 days', NULL, NULL, NULL),

  (1010, 'marco-rossi-1010', 'Marco Rossi', 'Data Scientist', 'Adevinta',
   '<p>Data scientist working on marketplace ranking. I mentor analysts leveling up to DS and engineers adding ML to their toolkit.</p>',
   '<ul><li><p>from analytics to data science;</p></li><li><p>ML system design basics;</p></li><li><p>practical experimentation and A/B testing.</p></li></ul>',
   'Machine learning, experimentation, Python, SQL, ranking systems',
   '2-5', '$30', 'active', 'marco.rossi@example.com', NULL, false, 100,
   'frame', now() - interval '2 days', now() - interval '1 day', NULL, NULL, NULL),

  (1011, 'lena-fischer-1011', 'Lena Fischer', 'HR Business Partner', 'SAP',
   '<p>HRBP at SAP with a decade in tech recruiting before that. I mentor on career moves, compensation conversations and interviews — from the other side of the table.</p>',
   '<ul><li><p>negotiating offers and promotions;</p></li><li><p>interview preparation (behavioral);</p></li><li><p>working with HR instead of around it.</p></li></ul>',
   'Compensation, interviewing, career development, people processes',
   '10+', 'Free', 'active', 'lena.fischer@example.com', NULL, false, 110,
   'frame', now() - interval '75 days', now() - interval '74 days', NULL, NULL, NULL),

  (1012, 'tom-baker-1012', 'Tom Baker', 'QA Engineer', 'Booking.com',
   '<p>QA engineer at Booking.com moving our teams from manual regression to test automation. I mentor testers starting out and developers who want to test better.</p>',
   '<ul><li><p>test automation foundations;</p></li><li><p>getting your first QA role;</p></li><li><p>quality practices in agile teams.</p></li></ul>',
   'Test automation, Playwright, quality processes, exploratory testing',
   '2-5', '$20', 'active', 'tom.baker@example.com', NULL, false, 120,
   'frame', now() - interval '30 days', now() - interval '29 days', NULL, NULL, NULL),

  -- Workflow fixtures ----------------------------------------------------
  (1013, 'nina-petrova-1013', 'Nina Petrova', 'Backend Engineer', 'Freelance',
   '<p>Backend engineer offering mentorship. Check out my services at my website!</p>',
   '<ul><li><p>backend development;</p></li><li><p>my consulting packages.</p></li></ul>',
   'Go, PostgreSQL, consulting',
   '5-10', '$40', 'draft', 'nina.petrova@example.com', NULL, false, 130,
   'frame', now() - interval '2 days', NULL, 'dev-login-draft', now() + interval '365 days',
   'The "About" section reads as a service ad rather than a mentoring offer. Describe what you''ll help mentees with and remove the consulting links — your listed price field covers that.'),

  (1014, 'oleg-sokolov-1014', 'Oleg Sokolov', 'DevOps Engineer', 'GitLab',
   '<p>DevOps engineer at GitLab. CI/CD, IaC and platform migrations — I''ve broken and fixed them all.</p>',
   '<ul><li><p>CI/CD pipeline design;</p></li><li><p>Terraform and IaC practices;</p></li><li><p>moving into DevOps roles.</p></li></ul>',
   'CI/CD, Terraform, GitLab, AWS, platform migrations',
   '5-10', '$40', 'pending', 'oleg.sokolov@example.com', NULL, false, 140,
   'frame', now() - interval '1 day', NULL, NULL, NULL, NULL)
ON CONFLICT (slug) DO NOTHING;

-- Tags -------------------------------------------------------------------
INSERT INTO mentor_tags (mentor_id, tag_id)
SELECT m.id, t.id
FROM (VALUES
  ('jonas-weber-1001',      'Team Lead/Management'),
  ('jonas-weber-1001',      'Backend'),
  ('amara-okafor-1002',     'Backend'),
  ('amara-okafor-1002',     'System Design'),
  ('ahmed-hassan-1003',     'iOS'),
  ('ahmed-hassan-1003',     'Android'),
  ('ingrid-johansson-1004', 'UX/UI/Design'),
  ('sean-obrien-1005',      'DevOps/SRE'),
  ('sean-obrien-1005',      'Cloud'),
  ('sean-obrien-1005',      'System Design'),
  ('priya-sharma-1006',     'UX/UI/Design'),
  ('priya-sharma-1006',     'Career'),
  ('yuki-tanaka-1007',      'DevOps/SRE'),
  ('elif-kaya-1008',        'Product Management'),
  ('elif-kaya-1008',        'Agile'),
  ('daria-kovalenko-1009',  'Frontend'),
  ('daria-kovalenko-1009',  'Code Review'),
  ('marco-rossi-1010',      'Data Science/ML'),
  ('marco-rossi-1010',      'Analytics'),
  ('lena-fischer-1011',     'HR'),
  ('lena-fischer-1011',     'Career'),
  ('lena-fischer-1011',     'Interview prep'),
  ('tom-baker-1012',        'QA'),
  ('nina-petrova-1013',     'Backend'),
  ('oleg-sokolov-1014',     'DevOps/SRE')
) AS seed(slug, tag_name)
JOIN mentors m ON m.slug = seed.slug
JOIN tags t ON t.name = seed.tag_name
ON CONFLICT DO NOTHING;

-- Completed sessions (drives sessionsCount) -------------------------------
INSERT INTO client_requests (mentor_id, email, name, description, level, status, status_changed_at)
SELECT m.id,
       'mentee' || gs || '@example.com',
       'Sample Mentee ' || gs,
       'Seeded completed mentorship for the dev catalog.',
       'Middle',
       'done',
       now() - (gs || ' days')::interval
FROM (VALUES
  ('jonas-weber-1001', 23),
  ('ahmed-hassan-1003', 7),
  ('sean-obrien-1005', 40),
  ('priya-sharma-1006', 31),
  ('elif-kaya-1008', 18),
  ('daria-kovalenko-1009', 5),
  ('lena-fischer-1011', 12),
  ('tom-baker-1012', 2)
) AS seed(slug, sessions)
JOIN mentors m ON m.slug = seed.slug
CROSS JOIN generate_series(1, seed.sessions) AS gs
-- guard: only seed once per mentor
WHERE NOT EXISTS (
  SELECT 1 FROM client_requests cr
  WHERE cr.mentor_id = m.id AND cr.email = 'mentee1@example.com'
);

-- A few open requests for the dashboard inbox of the active login mentor --
INSERT INTO client_requests (mentor_id, email, name, description, level, status, preferred_contact)
SELECT m.id, seed.email, seed.name, seed.description, seed.level, seed.status, seed.contact
FROM (VALUES
  ('daria.kova@example.com', 'Daria K.',
   'I''m a senior engineer at a fintech startup, and I''ve been offered a team-lead role. I''d like 2–3 sessions to decide whether to take it and how to negotiate the transition.',
   'Senior', 'pending', 'Telegram: @dariak'),
  ('mike.chen@example.com', 'Mike Chen',
   'Looking for help preparing an engineering-manager interview loop at a FAANG-adjacent company.',
   'Senior', 'contacted', NULL),
  ('sara.lindt@example.com', 'Sara Lindt',
   'First-time lead of a 4-person team, struggling with delegation. Would love ongoing monthly sessions.',
   'Middle', 'working', NULL)
) AS seed(email, name, description, level, status, contact)
JOIN mentors m ON m.slug = 'jonas-weber-1001'
WHERE NOT EXISTS (
  SELECT 1 FROM client_requests cr
  WHERE cr.mentor_id = m.id AND cr.email = 'daria.kova@example.com'
);

COMMIT;
