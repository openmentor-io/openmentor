-- phase: expand
-- (additive: roles and grants only; every pre-H8 DSN keeps working until an
-- operator switches it, so rollback may cross it)
-- Split database identities (SECURITY H8, DECISIONS D67).
--
-- Until now migrate, backend and worker all authenticated as POSTGRES_USER —
-- the image's BOOTSTRAP SUPERUSER. One SQL injection in the API was therefore
-- also `COPY ... FROM PROGRAM` (command execution in the postgres container)
-- and `pg_read_file()` (the whole data directory). Superuser was never
-- required: the only privileged DDL in this tree is `CREATE EXTENSION
-- pgcrypto` / `citext` (000001), and both are *trusted* in PG13+, so a role
-- with CREATE on the database can install them.
--
-- This file is ADDITIVE, idempotent, and changes NOTHING about how the running
-- services authenticate:
--
--   * the roles are created NOLOGIN and passwordless, so nothing can connect as
--     them until an operator sets a password — see
--     ../../docs/runbooks/database-identities.md for the sequenced switch;
--   * nothing is revoked from the existing superuser, so applying this cannot
--     lock a live service out. Retiring the superuser DSN is an operator step,
--     not a migration;
--   * object ownership moves to om_migrate. That is invisible to the running
--     app (a superuser bypasses privilege checks) and is precisely what lets a
--     FUTURE migration run as om_migrate rather than as superuser.
--
-- Re-running is safe BY DESIGN and that constrains what may go in here: the
-- ALTER ROLE below sets only the security attributes and deliberately never
-- touches LOGIN or PASSWORD, so re-applying this file after the DSN switch
-- cannot lock out a live service. Do not add `NOLOGIN` to it.
--
-- ===========================================================================
-- MERGE ORDER: 000010 (PR #75) -> 000011 (PR #77) -> 000012 (THIS FILE, #78).
-- ===========================================================================
-- These three migrations are on three branches open at the same time, and
-- `api/pkg/db/migrate.go` runs golang-migrate's `m.Up()`, which applies only
-- versions GREATER THAN the version recorded in `schema_migrations`. So if this
-- file merges and deploys FIRST, production's schema_migrations lands at 12 and
-- 000010/000011 are then SILENTLY NEVER APPLIED there — while every fresh
-- database (dev, CI, a staging clone, a DR rebuild) applies them, because those
-- start from version 0. Nothing errors: `migrate` exits 0, the deploy is green,
-- and the divergence only surfaces when the app code from those PRs hits a
-- production schema that never got their tables and columns.
--
-- Merging out of order is therefore recoverable ONLY by renumbering the skipped
-- migration before its deploy, or by forcing the version by hand on the VM.
-- Re-check the recorded version and the sibling PRs at merge time (D67). PR #73
-- adds the CI guard that fails a PR whose migration version is not greater than
-- the highest version on `origin/main`, so this comment is the record, not the
-- enforcement.
--
-- Separately, and for the same reason a version can be skipped without an error:
-- this file CANNOT be applied by om_migrate itself. CREATE ROLE / ALTER ROLE and
-- GRANT pg_read_all_data need CREATEROLE or superuser, which the migrator
-- correctly does not have — so a FRESH cluster must run the migration set as the
-- bootstrap superuser once before MIGRATE_DATABASE_URL may point at om_migrate.
-- See the fresh-database bullet in ../../docs/runbooks/database-identities.md;
-- case 9 of infra/db-identity-test.sh asserts it.

-- 1. The identities ---------------------------------------------------------
-- om_migrate    owns the schema and runs migrations (DDL)
-- om_api        cmd/api           — DML only
-- om_worker     cmd/worker        — DML only
-- om_backup     postgres-backup   — read only (pg_dump)
-- om_monitor_ro grant-only group role: the scoped read set for Grafana's
--               monitoring user, so "what monitoring may read" is reviewable
--               SQL rather than a `pg_read_all_data` that exposes every mentor
--               and client email. Granting it to grafana_monitoring and
--               revoking pg_read_all_data is an operator step (it narrows a
--               LIVE integration), see the runbook.
DO $$
DECLARE
    r text;
BEGIN
    FOREACH r IN ARRAY ARRAY['om_migrate', 'om_api', 'om_worker', 'om_backup', 'om_monitor_ro']
    LOOP
        IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            EXECUTE format('CREATE ROLE %I NOLOGIN', r);
        END IF;
        -- Self-healing for the attributes that matter, on every run. No LOGIN
        -- or PASSWORD clause here (see the header).
        EXECUTE format(
            'ALTER ROLE %I NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS',
            r);
    END LOOP;
END $$;

-- 2. Ownership of the existing schema objects -------------------------------
-- om_migrate must own what it will later ALTER. Extension members (the citext
-- type, the pgcrypto functions) and sequences owned by a table are skipped:
-- the former belong to the extension, the latter follow their table's owner
-- automatically and refuse a direct ALTER.
DO $$
DECLARE
    obj record;
BEGIN
    FOR obj IN
        SELECT format('%s %s',
                      CASE c.relkind
                          WHEN 'r' THEN 'TABLE'
                          WHEN 'p' THEN 'TABLE'
                          WHEN 'S' THEN 'SEQUENCE'
                          WHEN 'v' THEN 'VIEW'
                          WHEN 'm' THEN 'MATERIALIZED VIEW'
                      END,
                      c.oid::regclass) AS target
        FROM pg_class c
        JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public'
          AND c.relkind IN ('r', 'p', 'S', 'v', 'm')
          AND c.relowner <> 'om_migrate'::regrole
          AND NOT EXISTS (
              SELECT 1 FROM pg_depend d
              WHERE d.classid = 'pg_class'::regclass
                AND d.objid = c.oid
                AND d.deptype IN ('a', 'i', 'e')
          )
        -- Tables before sequences: ALTER TABLE carries its owned sequences.
        ORDER BY CASE c.relkind WHEN 'S' THEN 2 ELSE 1 END
    LOOP
        EXECUTE format('ALTER %s OWNER TO om_migrate', obj.target);
    END LOOP;

    -- set_updated_at() and anything else added later; ALTER ROUTINE covers
    -- functions and procedures alike.
    FOR obj IN
        SELECT p.oid::regprocedure AS target
        FROM pg_proc p
        JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'public'
          AND p.proowner <> 'om_migrate'::regrole
          AND NOT EXISTS (
              SELECT 1 FROM pg_depend d
              WHERE d.classid = 'pg_proc'::regclass
                AND d.objid = p.oid
                AND d.deptype = 'e'
          )
    LOOP
        EXECUTE format('ALTER ROUTINE %s OWNER TO om_migrate', obj.target);
    END LOOP;
END $$;

-- 3. Schema and database privileges -----------------------------------------
-- CREATE on the database is what makes the migrator able to install a TRUSTED
-- extension without being superuser — the single reason the app was superuser
-- in the first place. It does NOT allow dropping the database or the schema
-- (both need ownership).
DO $$
BEGIN
    EXECUTE format('GRANT CONNECT, CREATE ON DATABASE %I TO om_migrate', current_database());
    EXECUTE format('GRANT CONNECT ON DATABASE %I TO om_api, om_worker, om_backup', current_database());
END $$;

GRANT USAGE, CREATE ON SCHEMA public TO om_migrate;
GRANT USAGE ON SCHEMA public TO om_api, om_worker, om_backup, om_monitor_ro;

-- 4. DML for the two application identities ---------------------------------
-- No TRUNCATE, no REFERENCES, no TRIGGER, and no CREATE anywhere: om_api and
-- om_worker can read and write rows and nothing else.
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO om_api, om_worker;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO om_api, om_worker;

-- golang-migrate's bookkeeping table is not application data. Read is useful
-- (a version check), write is not. This narrows the grant made two lines
-- above, so it does not remove any privilege that predates this migration.
DO $$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        REVOKE INSERT, UPDATE, DELETE, TRUNCATE ON TABLE public.schema_migrations
            FROM om_api, om_worker;
    END IF;
END $$;

-- 5. Future objects ---------------------------------------------------------
-- Default privileges are per-creating-role, so both identities that can create
-- a table here need an entry: the CURRENT one (still the bootstrap superuser
-- until the operator switches migrate's DSN) and om_migrate (after). Without
-- both, whichever one is not listed would create tables om_api cannot read.
DO $$
DECLARE
    creator text;
BEGIN
    FOREACH creator IN ARRAY ARRAY[current_user, 'om_migrate']
    LOOP
        EXECUTE format(
            'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public '
            'GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO om_api, om_worker', creator);
        EXECUTE format(
            'ALTER DEFAULT PRIVILEGES FOR ROLE %I IN SCHEMA public '
            'GRANT USAGE, SELECT ON SEQUENCES TO om_api, om_worker', creator);
    END LOOP;
END $$;

-- 6. Backup identity --------------------------------------------------------
-- pg_read_all_data is SELECT on everything present and future, which is what
-- pg_dump needs and no more: it carries no COPY ... FROM/TO PROGRAM (that is
-- pg_execute_server_program) and no write anywhere.
GRANT pg_read_all_data TO om_backup;

-- 7. Scoped monitoring read set ---------------------------------------------
-- Deliberately an explicit list, not "all tables": a new table has to be added
-- here on purpose, so monitoring cannot silently acquire the next PII column.
-- Excluded: mentors, client_requests, moderators (emails, login tokens),
-- reviews (free-text feedback tied to a request).
DO $$
DECLARE
    t text;
BEGIN
    FOREACH t IN ARRAY ARRAY['tags', 'mentor_tags', 'mentor_slug_history', 'migration_intents', 'schema_migrations']
    LOOP
        IF to_regclass('public.' || quote_ident(t)) IS NOT NULL THEN
            EXECUTE format('GRANT SELECT ON TABLE public.%I TO om_monitor_ro', t);
        END IF;
    END LOOP;
END $$;
