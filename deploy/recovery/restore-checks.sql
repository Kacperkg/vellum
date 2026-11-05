-- All checks run before the restore is attested. Emit no row values or totals.
BEGIN TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY;
SET LOCAL search_path = pg_catalog, public;
SET LOCAL statement_timeout = '150s';
SET LOCAL lock_timeout = '5s';
DO $restore$
DECLARE
    seq record;
    last_value numeric;
    was_called boolean;
    next_value numeric;
    data_edge numeric;
BEGIN
    IF EXISTS (SELECT FROM pg_constraint WHERE connamespace = 'public'::regnamespace AND NOT convalidated)
       OR EXISTS (SELECT FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
                  WHERE c.relnamespace = 'public'::regnamespace AND NOT (i.indisvalid AND i.indisready AND i.indislive))
       OR NOT EXISTS (
           SELECT FROM pg_index i
           JOIN pg_class c ON c.oid = i.indexrelid
           JOIN pg_attribute u ON u.attrelid = i.indrelid AND u.attname = 'user_id'
           JOIN pg_attribute cat ON cat.attrelid = i.indrelid AND cat.attname = 'category_id'
           WHERE i.indrelid = 'public.budgets'::regclass AND c.relname = 'idx_budgets_user_category'
             AND i.indisunique AND i.indisvalid AND i.indpred IS NULL AND i.indexprs IS NULL
             AND i.indnkeyatts = 2 AND i.indkey[0] = u.attnum AND i.indkey[1] = cat.attnum)
       OR EXISTS (SELECT FROM public.budgets GROUP BY user_id, category_id HAVING count(*) > 1)
    THEN
        RAISE EXCEPTION 'invalid restored constraints or indexes';
    END IF;

    IF EXISTS (SELECT FROM public.accounts a LEFT JOIN public.users u ON u.id = a.user_id WHERE u.id IS NULL)
       OR EXISTS (SELECT FROM public.user_settings s LEFT JOIN public.users u ON u.id = s.user_id WHERE u.id IS NULL)
       OR EXISTS (
           SELECT FROM public.budgets b LEFT JOIN public.users u ON u.id = b.user_id
           LEFT JOIN public.categories c ON c.id = b.category_id WHERE u.id IS NULL OR c.id IS NULL)
       OR EXISTS (
           SELECT FROM public.goals g LEFT JOIN public.users u ON u.id = g.user_id
           LEFT JOIN public.accounts a ON a.id = g.account_id
           WHERE u.id IS NULL OR (g.account_id IS NOT NULL AND a.user_id IS DISTINCT FROM g.user_id))
       OR EXISTS (
           SELECT FROM public.transactions t LEFT JOIN public.accounts a ON a.id = t.account_id
           LEFT JOIN public.categories c ON c.id = t.category_id
           WHERE a.user_id IS DISTINCT FROM t.user_id OR c.id IS NULL)
       OR EXISTS (
           SELECT FROM public.transfers t LEFT JOIN public.accounts a ON a.id = t.from_account_id
           LEFT JOIN public.accounts b ON b.id = t.to_account_id
           WHERE a.user_id IS DISTINCT FROM t.user_id OR b.user_id IS DISTINCT FROM t.user_id
              OR t.from_account_id = t.to_account_id)
    THEN
        RAISE EXCEPTION 'invalid restored ownership';
    END IF;

    IF EXISTS (SELECT FROM public.users WHERE auth_version < 0)
       OR EXISTS (
           SELECT FROM public.refresh_tokens r LEFT JOIN public.users u ON u.id = r.user_id
           WHERE u.id IS NULL OR r.auth_version < 0 OR r.auth_version > u.auth_version
              OR (r.revoked_at IS NULL AND r.auth_version <> u.auth_version)
              OR r.token_hash !~ '^[0-9a-f]{64}$'
              OR r.family_id = '00000000-0000-0000-0000-000000000000'::uuid)
       OR EXISTS (
           SELECT FROM public.refresh_tokens GROUP BY coalesce(family_id, id)
           HAVING count(DISTINCT user_id) > 1 OR count(DISTINCT auth_version) > 1
              OR count(*) FILTER (WHERE revoked_at IS NULL) > 1)
       OR EXISTS (
           SELECT FROM public.password_reset_tokens p LEFT JOIN public.users u ON u.id = p.user_id
           WHERE u.id IS NULL OR p.token_hash !~ '^[0-9a-f]{64}$')
    THEN
        RAISE EXCEPTION 'invalid restored authentication relationships';
    END IF;

    IF EXISTS (
           SELECT name FROM (VALUES ('Groceries'), ('Dining'), ('Transport'), ('Housing'),
               ('Utilities'), ('Entertainment'), ('Shopping'), ('Health'), ('Travel'),
               ('Subscriptions'), ('Income')) AS canonical(name)
           EXCEPT SELECT name FROM public.categories)
       OR EXISTS (
           SELECT FROM public.transactions WHERE amount NOT BETWEEN 1 AND 9007199254740991
               OR type NOT IN ('income', 'expense')
               OR frequency NOT IN ('one-off', 'daily', 'weekly', 'monthly', 'yearly'))
       OR EXISTS (SELECT FROM public.transfers WHERE amount NOT BETWEEN 1 AND 9007199254740991)
       OR EXISTS (SELECT FROM public.budgets WHERE allocated_amount NOT BETWEEN 1 AND 9007199254740991)
       OR EXISTS (
           SELECT FROM public.goals WHERE target_amount NOT BETWEEN 1 AND 9007199254740991
              OR saved_amount NOT BETWEEN 0 AND target_amount
              OR monthly_contribution NOT BETWEEN 0 AND 9007199254740991)
    THEN
        RAISE EXCEPTION 'invalid restored monetary values';
    END IF;

    IF EXISTS (
        WITH effects AS (
            SELECT account_id, CASE type WHEN 'income' THEN amount::numeric ELSE -amount::numeric END AS amount
            FROM public.transactions
            UNION ALL SELECT from_account_id, -amount::numeric FROM public.transfers
            UNION ALL SELECT to_account_id, amount::numeric FROM public.transfers
        ), balances AS (
            SELECT account_id, sum(amount) AS balance FROM effects GROUP BY account_id
        ), goals_by_account AS (
            SELECT account_id, sum(target_amount::numeric) AS allocated
            FROM public.goals WHERE account_id IS NOT NULL GROUP BY account_id
        )
        SELECT FROM public.accounts a
        LEFT JOIN balances b ON b.account_id = a.id
        LEFT JOIN goals_by_account g ON g.account_id = a.id
        WHERE abs(coalesce(b.balance, 0)) > 9007199254740991
           OR coalesce(g.allocated, 0) > 9007199254740991
           OR abs(coalesce(b.balance, 0) - coalesce(g.allocated, 0)) > 9007199254740991
    ) THEN
        RAISE EXCEPTION 'restored account aggregate exceeds supported precision';
    END IF;

    -- Current Vellum application keys are UUIDs. Any future public sequence
    -- must be non-cycling, owned by a column, and have a safe next value.
    FOR seq IN
        SELECT c.relname, s.seqmin, s.seqmax, s.seqincrement, s.seqcycle,
               tn.nspname AS table_schema, t.relname AS table_name, a.attname AS column_name
        FROM pg_class c JOIN pg_sequence s ON s.seqrelid = c.oid
        LEFT JOIN pg_depend d ON d.objid = c.oid AND d.classid = 'pg_class'::regclass
             AND d.refclassid = 'pg_class'::regclass AND d.deptype IN ('a', 'i')
        LEFT JOIN pg_class t ON t.oid = d.refobjid
        LEFT JOIN pg_namespace tn ON tn.oid = t.relnamespace
        LEFT JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = d.refobjsubid
        WHERE c.relnamespace = 'public'::regnamespace
    LOOP
        IF seq.seqcycle OR seq.table_schema IS DISTINCT FROM 'public' OR seq.column_name IS NULL THEN
            RAISE EXCEPTION 'unsupported restored sequence';
        END IF;
        EXECUTE format('SELECT last_value, is_called FROM public.%I', seq.relname)
            INTO last_value, was_called;
        next_value := last_value + CASE WHEN was_called THEN seq.seqincrement ELSE 0 END;
        EXECUTE format('SELECT %s(%I)::numeric FROM public.%I',
            CASE WHEN seq.seqincrement > 0 THEN 'max' ELSE 'min' END, seq.column_name, seq.table_name)
            INTO data_edge;
        IF next_value NOT BETWEEN seq.seqmin AND seq.seqmax OR
           (seq.seqincrement > 0 AND next_value <= data_edge) OR
           (seq.seqincrement < 0 AND next_value >= data_edge) THEN
            RAISE EXCEPTION 'unsafe restored sequence position';
        END IF;
    END LOOP;
END
$restore$;
ROLLBACK;
