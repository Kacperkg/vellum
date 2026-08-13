package integration_test

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/database"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repositories"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func integrationDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("TEST_DATABASE_DSN is not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate test database: %v", err)
	}
	return db
}

func TestTenantScopedRepositories(t *testing.T) {
	db := integrationDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })

	userA := &models.User{FirstName: "A", LastName: "User", Email: uuid.NewString() + "@example.com", PasswordHash: "hash"}
	userB := &models.User{FirstName: "B", LastName: "User", Email: uuid.NewString() + "@example.com", PasswordHash: "hash"}
	category := &models.Category{Name: "category-" + uuid.NewString()}
	for _, value := range []any{userA, userB, category} {
		if err := tx.Create(value).Error; err != nil {
			t.Fatalf("seed %T: %v", value, err)
		}
	}

	t.Run("account mutations", func(t *testing.T) {
		repository := repositories.NewGormAccountRepository(tx)
		account := &models.Account{UserID: userA.ID, Name: "Main"}
		if err := repository.Create(account); err != nil {
			t.Fatal(err)
		}
		account.Name = "Updated"
		if err := repository.Update(userB.ID, account); !errors.Is(err, repositories.ErrNotFound) {
			t.Fatalf("foreign update error = %v", err)
		}
		if err := repository.Update(userA.ID, account); err != nil {
			t.Fatal(err)
		}
		if err := repository.Delete(userB.ID, account.ID); !errors.Is(err, repositories.ErrNotFound) {
			t.Fatalf("foreign delete error = %v", err)
		}
	})

	accountA := &models.Account{UserID: userA.ID, Name: "A"}
	accountB := &models.Account{UserID: userB.ID, Name: "B"}
	if err := tx.Create([]*models.Account{accountA, accountB}).Error; err != nil {
		t.Fatal(err)
	}

	t.Run("budget mutations", func(t *testing.T) {
		repository := repositories.NewGormBudgetRepository(tx)
		budget := &models.Budget{UserID: userA.ID, CategoryID: category.ID, AllocatedAmount: 1000}
		if err := repository.Create(budget); err != nil {
			t.Fatal(err)
		}
		budget.AllocatedAmount = 2000
		if err := repository.Update(userA.ID, budget); err != nil {
			t.Fatal(err)
		}
		if err := repository.Delete(userB.ID, budget.ID); !errors.Is(err, repositories.ErrNotFound) {
			t.Fatalf("foreign delete error = %v", err)
		}
	})

	t.Run("goal zero-value update is persisted", func(t *testing.T) {
		repository := repositories.NewGormGoalRepository(tx)
		goal := &models.Goal{UserID: userA.ID, Name: "Goal", TargetAmount: 1000, SavedAmount: 100, MonthlyContribution: 50}
		if err := repository.Create(goal); err != nil {
			t.Fatal(err)
		}
		goal.MonthlyContribution = 0
		if err := repository.Update(userA.ID, goal); err != nil {
			t.Fatal(err)
		}
		found, err := repository.FindByID(goal.ID)
		if err != nil || found.MonthlyContribution != 0 {
			t.Fatalf("goal = %#v, error = %v", found, err)
		}
	})

	t.Run("transaction queries and mutations are tenant scoped", func(t *testing.T) {
		repository := repositories.NewGormTransactionRepository(tx)
		transactionA := &models.Transaction{UserID: userA.ID, Name: "A", Amount: 100, AccountID: accountA.ID, CategoryID: category.ID, Date: time.Now(), Frequency: models.OneOff, Type: models.Expense}
		transactionB := &models.Transaction{UserID: userB.ID, Name: "B", Amount: 100, AccountID: accountA.ID, CategoryID: category.ID, Date: time.Now(), Frequency: models.OneOff, Type: models.Expense}
		if err := repository.Create(transactionA); err != nil {
			t.Fatal(err)
		}
		if err := repository.Create(transactionB); err != nil {
			t.Fatal(err)
		}
		found, err := repository.FindByAccountID(userA.ID, accountA.ID)
		if err != nil || len(found) != 1 || found[0].UserID != userA.ID {
			t.Fatalf("transactions = %#v, error = %v", found, err)
		}
		if err := repository.Delete(userB.ID, transactionA.ID); !errors.Is(err, repositories.ErrNotFound) {
			t.Fatalf("foreign delete error = %v", err)
		}
	})

	t.Run("settings update is tenant scoped", func(t *testing.T) {
		repository := repositories.NewGormUserSettingsRepository(tx)
		settings := &models.UserSettings{UserID: userA.ID, Currency: models.GBP, DateFormat: models.DDMMYYYY, NumberFormat: models.CommaDecimal, WeekStartsOn: models.Monday}
		if err := repository.Create(settings); err != nil {
			t.Fatal(err)
		}
		settings.Currency = models.EUR
		if err := repository.Update(userB.ID, settings); !errors.Is(err, repositories.ErrNotFound) {
			t.Fatalf("foreign update error = %v", err)
		}
		if err := repository.Update(userA.ID, settings); err != nil {
			t.Fatal(err)
		}
	})
}
