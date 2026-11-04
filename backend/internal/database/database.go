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
	databaseConnectTimeout    = 5 * time.Second
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
	conn.ConnectTimeout = databaseConnectTimeout
	conn.RuntimeParams["statement_timeout"] = strconv.FormatInt(statementTimeout.Milliseconds(), 10)
	conn.RuntimeParams["lock_timeout"] = strconv.FormatInt(LockTimeout.Milliseconds(), 10)
	conn.RuntimeParams["idle_in_transaction_session_timeout"] = strconv.FormatInt(IdleTransactionTimeout.Milliseconds(), 10)
	return conn, nil
}

func Connect(cfg *config.Config) (*gorm.DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), databaseConnectTimeout)
	defer cancel()
	return connectWithPolicy(ctx, cfg, StatementTimeout, MaxOpenConnections, MaxIdleConnections)
}

// MigrateConfigured uses a separate, short-lived startup pool. Slow schema work
// never relaxes runtime session limits or occupies runtime connections.
func MigrateConfigured(ctx context.Context, cfg *config.Config) error {
	db, err := connectWithPolicy(ctx, cfg, MigrationStatementTimeout, 1, 1)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
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

func connectWithPolicy(ctx context.Context, cfg *config.Config, statementTimeout time.Duration, maxOpen, maxIdle int) (*gorm.DB, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
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
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent), DisableAutomaticPing: true},
	)
	if err == nil {
		err = sqlDB.PingContext(ctx)
	}
	if err != nil {
		_ = sqlDB.Close()
		return nil, err
	}
	return db, nil
}
