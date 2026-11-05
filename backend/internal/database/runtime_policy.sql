WITH expected (name, allowed) AS (
    VALUES ('users', ARRAY['SELECT', 'INSERT', 'UPDATE']),
           ('user_settings', ARRAY['SELECT', 'INSERT', 'UPDATE']),
           ('refresh_tokens', ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE']),
           ('password_reset_tokens', ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE']),
           ('accounts', ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE']),
           ('transactions', ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE']),
           ('transfers', ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE']),
           ('budgets', ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE']),
           ('goals', ARRAY['SELECT', 'INSERT', 'UPDATE', 'DELETE']),
           ('categories', ARRAY['SELECT']),
           ('vellum_schema_migrations', ARRAY['SELECT'])
), public_relations AS (
    SELECT c.*
    FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
    WHERE n.nspname = 'public'
)
SELECT
    EXISTS (
        SELECT FROM pg_roles WHERE rolname = 'vellum_runtime'
        AND rolcanlogin AND NOT (rolsuper OR rolcreatedb OR rolcreaterole OR
            rolreplication OR rolbypassrls OR rolinherit)
    )
    AND NOT EXISTS (
        SELECT FROM pg_roles WHERE rolname <> 'vellum_runtime'
        AND pg_has_role('vellum_runtime', oid, 'MEMBER')
    )
    AND NOT EXISTS (SELECT FROM pg_database WHERE datdba = 'vellum_runtime'::regrole)
    AND NOT has_database_privilege('vellum_runtime', current_database(), 'CREATE,TEMP')
    AND NOT EXISTS (
        SELECT FROM pg_database WHERE datallowconn AND datname <> current_database()
        AND has_database_privilege('vellum_runtime', oid, 'CONNECT')
    )
    AND has_schema_privilege('vellum_runtime', 'public', 'USAGE')
    AND NOT EXISTS (
        SELECT FROM pg_namespace
        WHERE has_schema_privilege('vellum_runtime', oid, 'CREATE')
        OR (nspname NOT IN ('public', 'information_schema') AND left(nspname, 3) <> 'pg_')
    )
    AND EXISTS (
        SELECT FROM pg_namespace n JOIN pg_roles r ON n.nspowner = r.oid
        WHERE n.nspname = 'public' AND r.rolname = 'vellum_migrator'
    )
    AND NOT EXISTS (SELECT FROM pg_extension WHERE extname <> 'plpgsql')
    AND NOT EXISTS (
        SELECT FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
        WHERE n.nspname = 'public'
        OR (n.nspname = 'pg_catalog' AND p.proname = ANY(ARRAY[
            'lo_create', 'lo_creat', 'lo_from_bytea', 'lo_put', 'lowrite',
            'lo_unlink', 'lo_truncate', 'lo_truncate64', 'lo_import'
        ]) AND has_function_privilege('vellum_runtime', p.oid, 'EXECUTE'))
    )
    AND NOT EXISTS (
        SELECT FROM public_relations c LEFT JOIN expected e ON c.relname = e.name
        WHERE c.relkind <> 'i' AND (e.name IS NULL OR c.relkind <> 'r')
    )
    AND NOT EXISTS (
        SELECT FROM expected e LEFT JOIN public_relations c ON c.relname = e.name
        WHERE c.oid IS NULL OR c.relkind <> 'r'
        OR pg_get_userbyid(c.relowner) <> 'vellum_migrator'
        OR c.relrowsecurity OR c.relhasrules
        OR EXISTS (SELECT FROM pg_trigger t WHERE t.tgrelid = c.oid AND NOT t.tgisinternal)
        OR EXISTS (
            SELECT FROM aclexplode(c.relacl) acl
            WHERE (acl.grantee = 0 OR acl.grantee = 'vellum_runtime'::regrole)
            AND (acl.is_grantable OR acl.privilege_type <> ALL(e.allowed))
        )
        OR EXISTS (
            SELECT FROM pg_attribute a, LATERAL aclexplode(a.attacl) acl
            WHERE a.attrelid = c.oid AND (acl.grantee = 0 OR acl.grantee = 'vellum_runtime'::regrole)
        )
        OR EXISTS (
            SELECT FROM unnest(e.allowed) privilege
            WHERE NOT has_table_privilege('vellum_runtime', c.oid, privilege)
        )
    ) AS restricted_runtime;
