package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"

	"github.com/kacperkg/vellum/internal/config"
	"gorm.io/gorm"
)

const (
	migrationVersion = 1
	migrationRole    = "vellum_migrator"
	runtimeRole      = "vellum_runtime"
	// Per-database advisory transaction lock. Host coordination and writer
	// drain are still required before invoking the maintenance command.
	migrationLock = 0x56454c4c554d0001
)

// migrationContractSHA256 is injected by the production image build. Running
// a binary built without a pinned contract cannot create or use the ledger.
var migrationContractSHA256 string

var contractDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func MigrationContract() (string, error) {
	if !contractDigestPattern.MatchString(migrationContractSHA256) {
		return "", errors.New("this binary has no supported migration contract")
	}
	return migrationContractSHA256, nil
}

// MigrateControlled applies the reviewed baseline in a single PostgreSQL
// transaction. It can transform old data or seed categories and is reserved
// for maintenance. A changed contract needs a separately reviewed version.
func MigrateControlled(ctx context.Context, cfg *config.Config) error {
	contract, err := MigrationContract()
	if err != nil {
		return err
	}
	if cfg.DBUser != migrationRole {
		return errors.New("controlled migration requires the dedicated migration login")
	}
	db, err := connectWithPolicy(ctx, cfg, MigrationStatementTimeout, 1, 1, "public")
	if err != nil {
		return errors.New("failed to connect to controlled migration database")
	}
	pool, err := db.DB()
	if err != nil {
		return errors.New("failed to initialise controlled migration connection pool")
	}
	defer pool.Close()
	return applyBaseline(db.WithContext(ctx), contract)
}

func applyBaseline(db *gorm.DB, contract string) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var dedicatedOwner bool
		if err := tx.Raw(`SELECT session_user = 'vellum_migrator'
			AND current_user = session_user
			AND (SELECT nspowner = (SELECT oid FROM pg_roles WHERE rolname = current_user)
				FROM pg_namespace WHERE nspname = 'public')`).Scan(&dedicatedOwner).Error; err != nil || !dedicatedOwner {
			return databaseDiagnostic("controlled migration requires the prepared application schema owner")
		}
		var locked bool
		if err := tx.Raw("SELECT pg_try_advisory_xact_lock(?)", int64(migrationLock)).Scan(&locked).Error; err != nil || !locked {
			return databaseDiagnostic("controlled migration lock is unavailable")
		}
		var hasLedger bool
		if err := tx.Raw("SELECT to_regclass('public.vellum_schema_migrations') IS NOT NULL").Scan(&hasLedger).Error; err != nil {
			return databaseDiagnostic("migration ledger is unavailable")
		}
		if hasLedger {
			applied, err := readMigrationLedger(tx, contract)
			if err != nil || applied {
				return err
			}
		} else if err := tx.Exec(`CREATE TABLE public.vellum_schema_migrations (
			version integer PRIMARY KEY CHECK (version > 0),
			contract_sha256 text NOT NULL CHECK (contract_sha256 ~ '^[0-9a-f]{64}$'),
			applied_at timestamptz NOT NULL DEFAULT transaction_timestamp()
		)`).Error; err != nil {
			return databaseDiagnostic("failed to initialise the migration ledger")
		}
		if err := Migrate(tx); err != nil {
			return databaseDiagnostic("baseline migration failed; review schema and legacy budget uniqueness privately")
		}
		if err := tx.Exec(runtimeGrantsSQL).Error; err != nil {
			return databaseDiagnostic("failed to install runtime table grants")
		}
		if err := tx.Exec(`INSERT INTO public.vellum_schema_migrations (version, contract_sha256)
			VALUES (?, ?)`, migrationVersion, contract).Error; err != nil {
			return databaseDiagnostic("failed to record the baseline migration")
		}
		return nil
	})
	if err != nil {
		// Database/driver errors during begin or commit can contain connection
		// information. Return fixed diagnostics even on an uncertain commit.
		return fmt.Errorf("controlled migration did not confirm commit; inspect the ledger before retrying: %s", migrationFailure(err))
	}
	return nil
}

func readMigrationLedger(db *gorm.DB, contract string) (bool, error) {
	var applied []struct {
		Version        int
		ContractSHA256 string
	}
	if err := db.Raw("SELECT version, contract_sha256 FROM public.vellum_schema_migrations LIMIT 2").Scan(&applied).Error; err != nil {
		return false, databaseDiagnostic("migration ledger is unavailable")
	}
	if len(applied) == 0 {
		return false, nil
	}
	if len(applied) != 1 || applied[0].Version != migrationVersion || applied[0].ContractSHA256 != contract {
		return false, databaseDiagnostic("migration ledger does not match the binary's reviewed contract")
	}
	return true, nil
}

func ConnectForServing(cfg *config.Config, restricted bool) (*gorm.DB, error) {
	if !restricted {
		db, err := Connect(cfg)
		if err != nil {
			return nil, databaseDiagnostic("failed to connect to database")
		}
		return db, nil
	}
	contract, err := MigrationContract()
	if err != nil {
		return nil, err
	}
	if cfg.DBUser != runtimeRole {
		return nil, databaseDiagnostic("serve requires the dedicated runtime login")
	}
	ctx, cancel := context.WithTimeout(context.Background(), databaseConnectTimeout)
	defer cancel()
	db, err := connectWithPolicy(ctx, cfg, StatementTimeout, MaxOpenConnections, MaxIdleConnections, "public")
	if err != nil {
		return nil, databaseDiagnostic("failed to connect to the runtime database")
	}
	if err := verifyServingDatabase(db.WithContext(ctx), contract); err != nil {
		if pool, poolErr := db.DB(); poolErr == nil {
			_ = pool.Close()
		}
		return nil, err
	}
	return db, nil
}

func verifyServingDatabase(db *gorm.DB, contract string) error {
	err := db.Transaction(func(tx *gorm.DB) error {
		var valid bool
		if err := tx.Raw("SELECT current_user = 'vellum_runtime' AND current_user = session_user").Scan(&valid).Error; err != nil || !valid {
			return databaseDiagnostic("runtime session identity does not match the serving policy")
		}
		if err := tx.Raw(runtimePolicySQL).Scan(&valid).Error; err != nil || !valid {
			return databaseDiagnostic("database privileges or objects do not match the serving policy")
		}
		applied, err := readMigrationLedger(tx, contract)
		if err != nil {
			return err
		}
		if !applied {
			return databaseDiagnostic("reviewed baseline migration has not been applied")
		}
		return nil
	}, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return databaseDiagnostic("runtime database verification failed: " + migrationFailure(err))
	}
	return nil
}

// Only fixed operational diagnostics may cross the process boundary. Errors
// from GORM or PostgreSQL can contain credentials or production values.
type databaseDiagnostic string

func (d databaseDiagnostic) Error() string { return string(d) }

func migrationFailure(err error) string {
	var diagnostic databaseDiagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.Error()
	}
	return "database transaction failed"
}
