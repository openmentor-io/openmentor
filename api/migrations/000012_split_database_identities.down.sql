-- Reverse of 000012 (SECURITY H8).
--
-- DANGER: this drops the roles the services may already be authenticating as.
-- Run it ONLY after every DSN has been pointed back at POSTGRES_USER and the
-- containers recreated — the "roll back" column of the table in
-- ../../docs/runbooks/database-identities.md. Dropping a role that a running
-- container still uses gives "password authentication failed" on the next
-- connection, not a graceful degradation.
--
-- Ownership is reassigned BEFORE the privilege cleanup on purpose: `DROP OWNED
-- BY om_migrate` would otherwise drop every table it now owns.

DO $$
DECLARE
    r text;
BEGIN
    IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'om_migrate') THEN
        REASSIGN OWNED BY om_migrate TO CURRENT_USER;
    END IF;

    FOREACH r IN ARRAY ARRAY['om_migrate', 'om_api', 'om_worker', 'om_backup', 'om_monitor_ro']
    LOOP
        IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = r) THEN
            -- Removes grants ON objects in this database and the ALTER DEFAULT
            -- PRIVILEGES entries; after the REASSIGN above it owns nothing.
            EXECUTE format('DROP OWNED BY %I', r);
            EXECUTE format('REVOKE ALL ON DATABASE %I FROM %I', current_database(), r);
            EXECUTE format('DROP ROLE %I', r);
        END IF;
    END LOOP;
END $$;

-- Default privileges recorded FOR the role that ran 000012 are removed with the
-- grantee, so no separate ALTER DEFAULT PRIVILEGES ... REVOKE is needed here.
