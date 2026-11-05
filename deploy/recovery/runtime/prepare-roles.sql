-- Reviewed, attended maintenance only. Connect directly to the one dedicated
-- Vellum database as the existing PostgreSQL bootstrap superuser.
-- Pass -v expected_database=... and keep psql output private.
\set ON_ERROR_STOP on
SELECT current_database() = :'expected_database'
       AND current_database() NOT IN ('postgres', 'template0', 'template1')
       AND NOT EXISTS (
           SELECT FROM pg_database
           WHERE datname NOT IN (current_database(), 'postgres', 'template0', 'template1')
       ) AS dedicated_database \gset
\if :dedicated_database
\else
DO $$ BEGIN RAISE EXCEPTION 'Expected the dedicated Vellum application database'; END $$;
\endif

BEGIN;
SET LOCAL lock_timeout = '2s';
SET LOCAL statement_timeout = '2min';
SET LOCAL search_path TO pg_catalog, public;
DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'vellum_migrator') THEN
        CREATE ROLE vellum_migrator LOGIN NOINHERIT;
    END IF;
    IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'vellum_runtime') THEN
        CREATE ROLE vellum_runtime LOGIN NOINHERIT;
    END IF;
    IF EXISTS (
        SELECT FROM pg_roles
        WHERE rolname IN ('vellum_migrator', 'vellum_runtime')
        AND (NOT rolcanlogin OR rolsuper OR rolcreatedb OR rolcreaterole OR
             rolreplication OR rolbypassrls OR rolinherit)
    ) OR EXISTS (
        SELECT FROM pg_auth_members
        WHERE roleid IN ('vellum_migrator'::regrole, 'vellum_runtime'::regrole)
           OR member IN ('vellum_migrator'::regrole, 'vellum_runtime'::regrole)
    ) THEN
        RAISE EXCEPTION 'Dedicated Vellum roles have unsafe existing attributes or membership';
    END IF;

    -- Refuse to guess how to transfer ownership of custom application objects.
    IF EXISTS (
        SELECT FROM pg_namespace
        WHERE nspname NOT IN ('public', 'information_schema') AND left(nspname, 3) <> 'pg_'
    ) OR EXISTS (
        SELECT FROM pg_extension WHERE extname <> 'plpgsql'
    ) OR EXISTS (
        SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace WHERE n.nspname = 'public'
    ) OR EXISTS (
        SELECT FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
        WHERE n.nspname = 'public' AND c.relkind <> 'i' AND (
            c.relkind <> 'r' OR c.relname NOT IN (
                'users', 'user_settings', 'refresh_tokens', 'password_reset_tokens',
                'accounts', 'transactions', 'transfers', 'budgets', 'goals',
                'categories', 'vellum_schema_migrations'
            )
        )
    ) THEN
        RAISE EXCEPTION 'Unexpected application schema objects require separate review';
    END IF;
END
$$;

-- The bootstrap superuser keeps database ownership and backup access. The
-- migration role owns only application schema/objects. The serving role cannot
-- connect to other databases or create schemas or temporary objects.
SELECT format('REVOKE CONNECT, CREATE, TEMP ON DATABASE %I FROM PUBLIC, vellum_runtime', current_database()) \gexec
SELECT format('GRANT CONNECT ON DATABASE %I TO vellum_migrator, vellum_runtime', current_database()) \gexec
REVOKE CONNECT ON DATABASE postgres, template1 FROM PUBLIC, vellum_runtime;
REVOKE ALL ON SCHEMA public FROM PUBLIC, vellum_runtime;
ALTER SCHEMA public OWNER TO vellum_migrator;
GRANT USAGE ON SCHEMA public TO vellum_runtime;

SELECT format('ALTER TABLE public.%I OWNER TO vellum_migrator', c.relname)
FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relkind = 'r'
ORDER BY c.relname \gexec
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM PUBLIC;

-- Large-object functions can create objects without CREATE on a schema.
SELECT format('REVOKE EXECUTE ON FUNCTION %s FROM PUBLIC, vellum_runtime',
              p.oid::regprocedure)
FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname = 'pg_catalog' AND p.proname = ANY(ARRAY[
    'lo_create', 'lo_creat', 'lo_from_bytea', 'lo_put', 'lowrite',
    'lo_unlink', 'lo_truncate', 'lo_truncate64', 'lo_import'
])
ORDER BY p.oid \gexec

COMMIT;
