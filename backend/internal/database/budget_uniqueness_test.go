package database

import (
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestMigrateBudgetUniquenessPreservesLegacyRecords(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, duplicate := range []bool{true, false} {
		name := "unique allocations migrate repeatedly"
		if duplicate {
			name = "duplicate allocations stop before migration without data loss"
		}
		t.Run(name, func(t *testing.T) {
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			t.Cleanup(func() { tx.Rollback() })
			schema := "budget_migration_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			if err := tx.Exec("CREATE SCHEMA " + schema).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec("SET LOCAL search_path TO " + schema).Error; err != nil {
				t.Fatal(err)
			}
			if err := tx.AutoMigrate(&models.User{}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Exec(`CREATE TABLE budgets (
				id uuid PRIMARY KEY, created_at timestamptz, updated_at timestamptz,
				user_id uuid NOT NULL, category_id uuid NOT NULL, allocated_amount bigint NOT NULL
			)`).Error; err != nil {
				t.Fatal(err)
			}
			user := &models.User{FirstName: "Legacy", LastName: "Budget", Email: uuid.NewString() + "@example.test", PasswordHash: "test-only-hash"}
			if err := tx.Create(user).Error; err != nil {
				t.Fatal(err)
			}
			categoryA, categoryB := uuid.New(), uuid.New()
			if duplicate {
				categoryB = categoryA
			}
			ids := []uuid.UUID{uuid.New(), uuid.New()}
			for i, category := range []uuid.UUID{categoryA, categoryB} {
				if err := tx.Exec(`INSERT INTO budgets (id,user_id,category_id,allocated_amount) VALUES (?,?,?,?)`, ids[i], user.ID, category, int64((i+1)*10000)).Error; err != nil {
					t.Fatal(err)
				}
			}
			err := Migrate(tx)
			if duplicate {
				if err == nil || !strings.Contains(err.Error(), "1 duplicate user/category groups") || !strings.Contains(err.Error(), "no records were changed") {
					t.Fatalf("migration error = %v", err)
				}
				if strings.Contains(err.Error(), user.ID.String()) || strings.Contains(err.Error(), categoryA.String()) {
					t.Fatalf("migration error exposes record identifiers: %v", err)
				}
				if tx.Migrator().HasTable(&models.Category{}) || tx.Migrator().HasIndex(&models.Budget{}, models.BudgetUserCategoryUniqueIndex) {
					t.Fatal("preflight failure changed the schema")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if err := Migrate(tx); err != nil {
					t.Fatalf("repeat migration: %v", err)
				}
				if !tx.Migrator().HasIndex(&models.Budget{}, models.BudgetUserCategoryUniqueIndex) {
					t.Fatal("unique budget index was not installed")
				}
			}
			for i, id := range ids {
				var budget models.Budget
				if err := tx.First(&budget, "id = ?", id).Error; err != nil || budget.AllocatedAmount != int64((i+1)*10000) || budget.UserID != user.ID {
					t.Fatalf("legacy allocation changed: %#v, error = %v", budget, err)
				}
			}
		})
	}
}
