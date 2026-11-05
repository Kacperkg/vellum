# Migration-free backend runtime

The production image accepts three explicit commands. `serve` starts the API
after read-only database checks. `migrate` is an attended, one-off maintenance
command. `migration-contract` prints the image's 64-character SHA-256 contract
without connecting to a database. The existing no-argument path remains for
standard deployments that still use startup migration.

Use `serve` with the dedicated `vellum_runtime` login and the usual API
configuration. Use `migrate` with the `vellum_migrator` login and only
`DB_HOST`, `DB_PORT`, `DB_NAME`, `DB_SSLMODE`, `DB_USER` and `DB_PASSWORD`.
Both use the existing target database. Keep the PostgreSQL bootstrap credentials
and migration credentials out of the serving container, its environment and
mounts. Use distinct role passwords in staging and production.

## First installation

1. Rehearse on an isolated PostgreSQL 17 restore using the exact immutable
   backend image. Record the source identity, role/ACL and schema fingerprints,
   migration contract, representative authentication and finance checks, and
   any schema or data effects. Verify recovery independently. Plan for the
   existing host-wide lock protocol, pending marker and API outage.
2. For an approved activation window, suspend overlapping work and drain all
   backend writers and target database clients under the host coordination
   procedure. Preserve the PostgreSQL container, volume, owner credentials and
   backup configuration. Run only reviewed copies of the operator SQL and
   immutable image. Never run checkout code with host root privileges.
3. Through the trusted database-admin session, run `prepare-roles.sql` with
   `psql -X -v expected_database=APP_DB -f REVIEWED_PREPARE_ROLES_SQL`.
   Supply real target values privately. This changes object ownership in the
   target application database and revokes public connections to the default
   administrative databases in that dedicated cluster. Verify access for any
   legitimate backup/monitoring role and grant only the required privileges
   under separate review. A shared cluster or extra schema/extension needs a
   separately designed ownership transition. Restrict database access to the
   target private network and SCRAM credentials. Set a distinct password for
   each new login using interactive psql `\password vellum_migrator` and
   `\password vellum_runtime`. Keep credentials in separate root-private files
   and avoid command arguments, logs and resolved Compose output.
4. Start the immutable backend image once with `migrate` and the migrator
   credential in a foreground, bounded container on the target internal
   database network only. Do not attach it to `web`, publish ports, mount
   production secrets or configure restarts. The container may report
   unhealthy if left running because the image health check expects an API.
   Success is command exit and the expected committed ledger row.
5. Run the reviewed query in
   [runtime_policy.sql](../../../backend/internal/database/runtime_policy.sql)
   as an independent database administrator. Require one true result. Query
   `SELECT version, contract_sha256 FROM public.vellum_schema_migrations LIMIT 2`
   and require one row with version 1 and the reviewed contract digest. Verify
   the schema and role evidence against the approved rehearsal and run the
   existing singleton, health and smoke checks with `serve` and only the
   runtime credential.
6. Configure the verified restricted profile and retained production baseline
   through the separate controller activation. Keep the pre-activation
   production path until this succeeds. After the transition, a historical
   image that only supports privileged startup needs separate reviewed
   recovery and cannot be used as an automatic rollback.

## Contract and failure handling

The image build hashes the reviewed database and migration source, model Go
files, module lockfiles and hash recipe. To independently compute the SHA-256,
run `sh migration-contract.sh INPUT_FILE` from a verified backend source
directory with a new scratch file outside the checkout. The production build
embeds the output in the binary. A bare development build has no contract
unless built through this recipe. Check the immutable image digest and the
independently calculated contract as separate inputs.

The version-1 baseline preserves the existing GORM migration and duplicate
budget guard. It can create or change constraints, indexes and tables, apply a
legacy transfer frequency default, and insert canonical categories. Treat it
as dynamic, potentially data-changing maintenance. The command acquires one
per-database transaction advisory lock, uses a two-second lock budget and a
two-minute per-statement budget, and gives the overall operation three minutes.
Schema changes, category seeding, table grants and the migration ledger commit
together. PostgreSQL rollbacks cover those transactional statements. Inspect
the ledger and quiesce the process after a lost connection, timeout or
interrupted commit. Retrying the same applied contract is a no-op. A different
hash or unknown ledger version fails closed and requires a reviewed versioned
migration. Do not rewrite the ledger or assume image rollback undoes changes.

Any future expansion of unattended admission to changed backend execution
requires an unchanged contract, verified effective role, runtime configuration
and cutover schema, and independent rehearsal of the exact digest against a
qualifying production snapshot. The controller's admission policy determines
which releases can use the restricted runtime automatically. Treat additive
DDL, constraint/index changes, destructive changes, data transformations,
seed changes and unclassified SQL as separate maintenance under this version.
The restricted login permits ordinary finance
and authentication writes, including password-reset expiry cleanup. It cannot
perform DDL, alter privileges or seed canonical categories. An end-state
comparison alone cannot establish how a candidate used its ordinary DML
privileges. The protected source review and rehearsal must cover its startup
behaviour. Production automation does not invoke `migrate`.

## Disposable PostgreSQL 17 validation

Create an empty, dedicated PostgreSQL 17 cluster and prepare the two roles
with the reviewed SQL above. Set `TEST_RUNTIME_ADMIN_DSN`,
`TEST_RUNTIME_MIGRATOR_DSN` and `TEST_RUNTIME_APP_DSN` to that same
disposable database with the appropriate test credentials. From `backend`,
run `go test -race ./internal/database -run TestControlledMigrationAndRestrictedRuntime -count=1`.
The test modifies schema, data and grants in this fixture. Use a separate
disposable database for the existing `TEST_DATABASE_DSN` integration suite.
