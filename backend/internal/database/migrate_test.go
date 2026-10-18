package database

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigrateSeedsCanonicalCategoriesIdempotently(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("first migration: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migration: %v", err)
	}

	for _, name := range canonicalCategoryNames {
		var count int64
		if err := db.Model(&models.Category{}).Where("name = ?", name).Count(&count).Error; err != nil {
			t.Fatalf("count category %q: %v", name, err)
		}
		if count != 1 {
			t.Fatalf("category %q count = %d, want 1", name, count)
		}
	}
}

func TestMigrateBackfillsLegacyTransferFrequency(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	// Exercise the old table shape in an isolated transactional schema; never
	// remove a column or change records in the application's active schema.
	schema := "transfer_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := tx.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("SET LOCAL search_path TO " + schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.User{}, &models.Account{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`CREATE TABLE transfers (
		id uuid PRIMARY KEY, created_at timestamptz, updated_at timestamptz,
		user_id uuid NOT NULL, name text NOT NULL, amount bigint NOT NULL,
		from_account_id uuid NOT NULL, to_account_id uuid NOT NULL, date timestamptz NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}
	user := &models.User{FirstName: "Legacy", LastName: "Transfer", Email: uuid.NewString() + "@example.test", PasswordHash: "hash"}
	if err := tx.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	from := &models.Account{UserID: user.ID, Name: "Source"}
	to := &models.Account{UserID: user.ID, Name: "Destination"}
	if err := tx.Create([]*models.Account{from, to}).Error; err != nil {
		t.Fatal(err)
	}
	id, date := uuid.New(), time.Now().UTC().Truncate(time.Second)
	if err := tx.Exec(`INSERT INTO transfers (id, user_id, name, amount, from_account_id, to_account_id, date) VALUES (?, ?, ?, ?, ?, ?, ?)`, id, user.ID, "Legacy funding", 12599, from.ID, to.ID, date).Error; err != nil {
		t.Fatal(err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("migrate legacy schema: %v", err)
	}
	if err := Migrate(tx); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	var found models.Transfer
	if err := tx.First(&found, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if found.Frequency != models.OneOff || found.Amount != 12599 || found.FromAccountID != from.ID || found.ToAccountID != to.ID || !found.Date.Equal(date) {
		t.Fatalf("legacy transfer changed or not backfilled: %#v", found)
	}
}

func TestMigrateLeavesLegacyGoalsUnlinked(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	schema := "goal_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := tx.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec("SET LOCAL search_path TO " + schema).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.AutoMigrate(&models.User{}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Exec(`CREATE TABLE goals (id uuid PRIMARY KEY, created_at timestamptz, updated_at timestamptz, user_id uuid NOT NULL, name text NOT NULL, target_amount bigint NOT NULL, saved_amount bigint NOT NULL, monthly_contribution bigint NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	user := &models.User{FirstName: "Legacy", LastName: "Goal", Email: uuid.NewString() + "@example.test", PasswordHash: "hash"}
	if err := tx.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if err := tx.Exec(`INSERT INTO goals (id,user_id,name,target_amount,saved_amount,monthly_contribution) VALUES (?,?,?,?,?,?)`, id, user.ID, "Trip", 120000, 10000, 5000).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := Migrate(tx); err != nil {
			t.Fatal(err)
		}
	}
	var goal models.Goal
	if err := tx.First(&goal, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	if goal.AccountID != nil || goal.TargetAmount != 120000 || goal.SavedAmount != 10000 || goal.MonthlyContribution != 5000 {
		t.Fatalf("legacy goal changed: %#v", goal)
	}
}
