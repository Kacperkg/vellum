package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kacperkg/vellum/internal/config"
	"gorm.io/gorm"
)

func TestConnectionResourcePolicy(t *testing.T) {
	cfg := &config.Config{DBHost: "localhost", DBPort: "5432", DBUser: "user name", DBPassword: "test-only ' password\\with?symbols", DBName: "vellum", DBSSLMode: "disable"}
	conn, err := connectionConfig(cfg)
	if err != nil {
		t.Fatal("parse connection configuration")
	}
	if conn.User != cfg.DBUser || conn.Password != cfg.DBPassword {
		t.Fatal("connection credentials were not preserved")
	}
	if conn.ConnectTimeout != 5*time.Second {
		t.Fatalf("connect timeout = %s", conn.ConnectTimeout)
	}
	for key, want := range map[string]string{"statement_timeout": "5000", "lock_timeout": "2000", "idle_in_transaction_session_timeout": "10000"} {
		if got := conn.RuntimeParams[key]; got != want {
			t.Fatalf("%s = %s, want %s", key, got, want)
		}
	}
}

func resourceConfig(t *testing.T) *config.Config {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	parsed, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatal("parse test database configuration")
	}
	return &config.Config{DBHost: parsed.Host, DBPort: strconv.Itoa(int(parsed.Port)), DBUser: parsed.User, DBPassword: parsed.Password, DBName: parsed.Database, DBSSLMode: "disable"}
}

func resourceDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := Connect(resourceConfig(t))
	if err != nil {
		t.Fatal("connect to test database")
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db
}

func TestMigrationConnectionFailureDoesNotExposeCredentials(t *testing.T) {
	cfg := &config.Config{DBHost: "localhost", DBPort: "invalid-port", DBUser: "test", DBPassword: "secret-must-not-be-logged", DBName: "test", DBSSLMode: "disable"}
	err := MigrateConfigured(context.Background(), cfg)
	if err == nil || strings.Contains(err.Error(), cfg.DBPassword) || err.Error() != "failed to connect to migration database" {
		t.Fatalf("unsafe migration connection error: %v", err)
	}
}

func TestPostgresMigrateConfiguredPreservesRuntimePolicy(t *testing.T) {
	cfg := resourceConfig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := MigrateConfigured(ctx, cfg); err != nil {
		t.Fatalf("startup migration failed: %v", err)
	}
	db := resourceDB(t)
	var got string
	if err := db.Raw("SHOW statement_timeout").Scan(&got).Error; err != nil || got != "5s" {
		t.Fatalf("runtime timeout=%q err=%v", got, err)
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if err := MigrateConfigured(canceled, cfg); err == nil {
		t.Fatal("canceled migration unexpectedly succeeded")
	}
	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("failed startup migration affected runtime pool: %v", err)
	}
}

func TestMigrationAndRuntimePoliciesAreSeparate(t *testing.T) {
	cfg := &config.Config{DBHost: "localhost", DBPort: "5432", DBUser: "test", DBName: "test", DBSSLMode: "disable"}
	migration, err := connectionConfigWithTimeout(cfg, MigrationStatementTimeout)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := connectionConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if migration.RuntimeParams["statement_timeout"] != "120000" || runtime.RuntimeParams["statement_timeout"] != "5000" {
		t.Fatal("startup and runtime statement policies are not isolated")
	}
}

func TestPostgresSessionResourceLimits(t *testing.T) {
	db := resourceDB(t)
	for key, want := range map[string]string{"statement_timeout": "5s", "lock_timeout": "2s", "idle_in_transaction_session_timeout": "10s"} {
		var got string
		if err := db.Raw("SELECT current_setting(?)", key).Scan(&got).Error; err != nil || got != want {
			t.Fatalf("%s=%q want %q err=%v", key, got, want, err)
		}
	}
	started := time.Now()
	err := db.Exec("SELECT pg_sleep(10)").Error
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "57014" {
		t.Fatalf("statement timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > 8*time.Second {
		t.Fatalf("statement timeout took %s", elapsed)
	}
	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("connection did not recover: %v", err)
	}
}

func TestPostgresDeadlineRollbackAndPoolCleanup(t *testing.T) {
	db := resourceDB(t)
	// A dedicated temporary table exercises rollback without touching application data.
	table := "resource_rollback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := db.Exec("CREATE TABLE " + table + " (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Exec("DROP TABLE " + table) })
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("INSERT INTO " + table + " VALUES (1)").Error; err != nil {
			return err
		}
		return tx.Exec("SELECT pg_sleep(10)").Error
	})
	if err == nil || ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("deadline did not cancel transaction: %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("cancellation did not interrupt query promptly")
	}
	var count int64
	if err := db.Table(table).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	pool, _ := db.DB()
	if pool.Stats().MaxOpenConnections != MaxOpenConnections {
		t.Fatalf("pool max=%d", pool.Stats().MaxOpenConnections)
	}
	connections := make([]*sql.Conn, 0, MaxOpenConnections)
	defer func() {
		for _, c := range connections {
			_ = c.Close()
		}
	}()
	for range MaxOpenConnections {
		c, err := pool.Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer waitCancel()
	if _, err := pool.Conn(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("pool wait cancellation=%v", err)
	}
	if pool.Stats().OpenConnections > MaxOpenConnections {
		t.Fatal("connection cap exceeded")
	}
	for _, c := range connections {
		_ = c.Close()
	}
	connections = nil
	if err := db.Exec("SELECT 1").Error; err != nil {
		t.Fatalf("pool did not recover: %v", err)
	}
}
