package database

import (
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// This test requires an empty disposable database with prepare-roles.sql
// installed. The roles must have distinct test credentials.
func TestControlledMigrationAndRestrictedRuntime(t *testing.T) {
	admin := runtimeTestConnection(t, "TEST_RUNTIME_ADMIN_DSN")
	migrator := runtimeTestConnection(t, "TEST_RUNTIME_MIGRATOR_DSN")
	runtime := runtimeTestConnection(t, "TEST_RUNTIME_APP_DSN")
	contract := strings.Repeat("a", 64)

	var empty bool
	if err := admin.Raw("SELECT to_regclass('public.users') IS NULL AND to_regclass('public.vellum_schema_migrations') IS NULL").Scan(&empty).Error; err != nil || !empty {
		t.Fatal("controlled migration test requires a fresh disposable database")
	}
	if err := verifyServingDatabase(runtime, contract); err == nil {
		t.Fatal("serving admitted an unprepared database")
	}

	lockHolder := admin.Begin()
	defer lockHolder.Rollback()
	mustExecRuntimeSQL(t, lockHolder, "SELECT pg_advisory_xact_lock(?)", int64(migrationLock))
	if err := applyBaseline(migrator, contract); err == nil || !strings.Contains(err.Error(), "lock is unavailable") {
		t.Fatalf("migration under contention returned %v", err)
	}
	if err := lockHolder.Rollback().Error; err != nil {
		t.Fatal(err)
	}

	// Force the final ledger insert to fail after real DDL, seeding and grants.
	// An earlier rollback would not prove that the side effects are atomic.
	mustExecRuntimeSQL(t, migrator, `CREATE TABLE public.vellum_schema_migrations (
		version integer PRIMARY KEY CHECK (version = 99),
		contract_sha256 text NOT NULL
	)`)
	if err := applyBaseline(migrator, contract); err == nil || !strings.Contains(err.Error(), "failed to record") {
		t.Fatalf("migration with rejected ledger write returned %v", err)
	}
	var rolledBack bool
	if err := admin.Raw("SELECT to_regclass('public.users') IS NULL AND to_regclass('public.categories') IS NULL").Scan(&rolledBack).Error; err != nil || !rolledBack {
		t.Fatal("baseline DDL survived a failed ledger write")
	}
	mustExecRuntimeSQL(t, migrator, "DROP TABLE public.vellum_schema_migrations")

	if err := applyBaseline(migrator, contract); err != nil {
		t.Fatal(err)
	}
	if err := applyBaseline(migrator, strings.Repeat("b", 64)); err == nil {
		t.Fatal("migration accepted a different baseline hash")
	}
	if err := verifyServingDatabase(runtime, strings.Repeat("b", 64)); err == nil {
		t.Fatal("serve accepted a different baseline hash")
	}
	if err := verifyServingDatabase(admin, contract); err == nil {
		t.Fatal("serve accepted the bootstrap role")
	}

	// A removed canonical seed remains removed across a no-op retry and
	// serving verification. Normal startup must not rerun canonical seeding.
	mustExecRuntimeSQL(t, migrator, "DELETE FROM public.categories WHERE name = 'Income'")
	if err := applyBaseline(migrator, contract); err != nil {
		t.Fatal(err)
	}
	if err := verifyServingDatabase(runtime, contract); err != nil {
		t.Fatal(err)
	}
	var missing bool
	if err := runtime.Raw("SELECT NOT EXISTS (SELECT FROM public.categories WHERE name = 'Income')").Scan(&missing).Error; err != nil || !missing {
		t.Fatal("idempotent retry or serving verification ran the seed")
	}

	for _, statement := range []string{
		"ALTER TABLE public.users ADD COLUMN forbidden integer",
		"CREATE TABLE public.forbidden (id integer)",
		"CREATE SCHEMA forbidden",
		"CREATE TEMP TABLE forbidden (id integer)",
		"CREATE DATABASE forbidden",
		"INSERT INTO public.categories (id, name) VALUES ('00000000-0000-4000-8000-000000000001', 'Forbidden')",
		"UPDATE public.categories SET name = 'Forbidden'",
		"TRUNCATE public.goals",
		"SELECT lo_create(0)",
		"UPDATE public.vellum_schema_migrations SET contract_sha256 = repeat('c', 64)",
		"SET ROLE vellum_migrator",
	} {
		var pgErr *pgconn.PgError
		if err := runtime.Exec(statement).Error; !errors.As(err, &pgErr) || pgErr.Code != "42501" {
			t.Fatalf("runtime statement %q was not rejected for insufficient privilege: %v", statement, err)
		}
	}
	if err := runtime.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO public.users (id, first_name, last_name, email, password_hash)
			VALUES ('00000000-0000-4000-8000-000000000002', 'Synthetic', 'User', 'synthetic@example.test', 'hash')`).Error; err != nil {
			return err
		}
		var email string
		if err := tx.Raw("SELECT email FROM public.users WHERE email = 'synthetic@example.test' FOR UPDATE").Scan(&email).Error; err != nil {
			return err
		}
		return tx.Exec("UPDATE public.users SET first_name = 'Updated' WHERE email = ?", email).Error
	}, &sql.TxOptions{}); err != nil {
		t.Fatalf("normal runtime writes and row locks failed: %v", err)
	}
	mustExecRuntimeSQL(t, migrator, "GRANT UPDATE (name) ON public.categories TO PUBLIC")
	if err := verifyServingDatabase(runtime, contract); err == nil {
		t.Fatal("serve accepted effective category write drift through PUBLIC")
	}
}

func runtimeTestConnection(t *testing.T, name string) *gorm.DB {
	t.Helper()
	dsn := os.Getenv(name)
	if dsn == "" {
		t.Skip(name + " is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal("could not connect to disposable test database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal("could not obtain disposable test database pool")
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db
}

func mustExecRuntimeSQL(t *testing.T, db *gorm.DB, statement string, args ...any) {
	t.Helper()
	if err := db.Exec(statement, args...).Error; err != nil {
		t.Fatalf("test setup query failed: %v", err)
	}
}
