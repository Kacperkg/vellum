-- Reviewed baseline grants. Applying these in a disposable restored database
-- recreates the application ACLs without running migration or seed logic.
DO $$
BEGIN
    GRANT SELECT, INSERT, UPDATE ON public.users, public.user_settings TO vellum_runtime;
    GRANT SELECT, INSERT, UPDATE, DELETE ON public.refresh_tokens, public.password_reset_tokens,
        public.accounts, public.transactions, public.transfers, public.budgets, public.goals TO vellum_runtime;
    GRANT SELECT ON public.categories, public.vellum_schema_migrations TO vellum_runtime;
END
$$;
