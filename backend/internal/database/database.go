package database

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/kacperkg/vellum/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	MaxOpenConnections        = 16
	MaxIdleConnections        = 8
	StatementTimeout          = 5 * time.Second
	LockTimeout               = 2 * time.Second
	IdleTransactionTimeout    = 10 * time.Second
	MigrationStatementTimeout = 2 * time.Minute
)

// connectionConfig installs limits in every PostgreSQL session, including
// replacement connections. Request contexts can impose earlier deadlines.
func connectionConfig(cfg *config.Config) (*pgx.ConnConfig, error) {
	return connectionConfigWithTimeout(cfg, StatementTimeout)
}

func connectionConfigWithTimeout(cfg *config.Config, statementTimeout time.Duration) (*pgx.ConnConfig, error) {
	dsn := url.URL{
		Scheme:   "postgres",
		Host:     net.JoinHostPort(cfg.DBHost, cfg.DBPort),
		User:     url.UserPassword(cfg.DBUser, cfg.DBPassword),
		Path:     "/" + cfg.DBName,
		RawQuery: url.Values{"sslmode": {cfg.DBSSLMode}}.Encode(),
	}
	conn, err := pgx.ParseConfig(dsn.String())
	if err != nil {
		return nil, err
	}
	conn.ConnectTimeout = 5 * time.Second
	conn.RuntimeParams["statement_timeout"] = strconv.FormatInt(statementTimeout.Milliseconds(), 10)
	conn.RuntimeParams["lock_timeout"] = strconv.FormatInt(LockTimeout.Milliseconds(), 10)
	conn.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.FormatInt(IdleTransactionTimeout.Milliseconds(), 10)
	return conn, nil
}

func Connect(cfg *config.Config) (*gorm.DB, error) {
	return connectWithPolicy(cfg, StatementTimeout, MaxOpenConnections, MaxIdleConnections)
}

// MigrateConfigured uses a separate, short-lived startup pool. Slow schema work
// never relaxes runtime session limits or occupies runtime connections.
func MigrateConfigured(ctx context.Context, cfg *config.Config) error {
	db, err := connectWithPolicy(cfg, MigrationStatementTimeout, 1, 1)
	if err != nil {
		// Driver parse errors may include the original credential-bearing URL.
		return errors.New("failed to connect to migration database")
	}
	pool, err := db.DB()
	if err != nil {
		return err
	}
	defer pool.Close()
	return Migrate(db.WithContext(ctx))
}

func connectWithPolicy(cfg *config.Config, statementTimeout time.Duration, maxOpen, maxIdle int) (*gorm.DB, error) {
	conn, err := connectionConfigWithTimeout(cfg, statementTimeout)
	if err != nil {
		return nil, err
	}
	sqlDB := stdlib.OpenDB(*conn)
	sqlDB.SetMaxOpenConns(maxOpen)
	sqlDB.SetMaxIdleConns(maxIdle)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)
	db, err := gorm.Open(
		postgres.New(postgres.Config{Conn: sqlDB}),
		// SQL errors can contain credentials, password hashes or recipient data.
		// Services and workers emit fixed operational messages instead.
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}
