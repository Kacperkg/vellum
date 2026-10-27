package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
)

func TestFinanceRepositoriesPropagateCancellation(t *testing.T) {
	db := integrationDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	userID := uuid.New()
	checks := map[string]func() error{
		"accounts": func() error {
			_, err := repository.NewAccountRepository(db).WithContext(ctx).FindByUserID(userID)
			return err
		},
		"budgets": func() error {
			_, err := repository.NewBudgetRepository(db).WithContext(ctx).FindByUserID(userID)
			return err
		},
		"categories": func() error { _, err := repository.NewCategoryRepository(db).WithContext(ctx).ListAll(); return err },
		"goals": func() error {
			_, err := repository.NewGoalRepository(db).WithContext(ctx).FindByUserID(userID)
			return err
		},
		"transactions": func() error {
			_, err := repository.NewTransactionRepository(db).WithContext(ctx).FindByUserID(userID)
			return err
		},
		"transfers": func() error {
			_, err := repository.NewTransferRepository(db).WithContext(ctx).FindByUserID(userID)
			return err
		},
		"settings": func() error {
			_, err := repository.NewUserSettingsRepository(db).WithContext(ctx).FindByUserID(userID)
			return err
		},
	}
	for name, check := range checks {
		t.Run(name, func(t *testing.T) {
			if err := check(); !errors.Is(err, context.Canceled) {
				t.Fatalf("error=%v, want canceled", err)
			}
		})
	}
	if _, err := repository.NewCategoryRepository(db).ListAll(); err != nil {
		t.Fatalf("shared repository poisoned: %v", err)
	}
}

func TestConcurrentTransfersWithCanceledMutationsPreserveLedger(t *testing.T) {
	db := integrationDB(t)
	user := &models.User{FirstName: "Limits", LastName: "Test", Email: uuid.NewString() + "@example.test", PasswordHash: "test-only-hash"}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	from := &models.Account{UserID: user.ID, Name: "From"}
	to := &models.Account{UserID: user.ID, Name: "To"}
	if err := db.Create([]*models.Account{from, to}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.Where("user_id = ?", user.ID).Delete(&models.Transfer{})
		db.Where("user_id = ?", user.ID).Delete(&models.Account{})
		db.Delete(user)
	})
	repo := repository.NewTransferRepository(db)
	results := make(chan error, 18)
	for i := range 18 {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if i >= 12 {
				cancel()
			}
			err := repo.WithContext(ctx).Create(&models.Transfer{UserID: user.ID, Name: "Funding", Amount: 100, FromAccountID: from.ID, ToAccountID: to.ID, Date: time.Now(), Frequency: models.OneOff})
			if i >= 12 && !errors.Is(err, context.Canceled) {
				results <- errors.New("canceled mutation did not return context.Canceled")
				return
			}
			if i >= 12 {
				results <- nil
				return
			}
			results <- err
		}()
	}
	for range 18 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	accounts := repository.NewAccountRepository(db)
	a, err := accounts.GetByID(from.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, err := accounts.GetByID(to.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Balance != -1200 || b.Balance != 1200 || a.Balance+b.Balance != 0 {
		t.Fatalf("unbalanced ledger: from=%d to=%d", a.Balance, b.Balance)
	}
	transfers, err := repo.FindByUserID(user.ID)
	if err != nil || len(transfers) != 12 {
		t.Fatalf("transfers=%d err=%v", len(transfers), err)
	}
	locked := db.Begin()
	if locked.Error != nil {
		t.Fatal(locked.Error)
	}
	defer locked.Rollback()
	if err := locked.Exec("SELECT id FROM transfers WHERE id = ? FOR UPDATE", transfers[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	changed := *transfers[0]
	changed.Amount = 99999
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := repo.WithContext(ctx).Update(user.ID, &changed); err == nil || ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("blocked update err=%v context=%v", err, ctx.Err())
	}
	if err := locked.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	unchanged, err := repo.FindByID(changed.ID)
	if err != nil || unchanged.Amount != 100 {
		t.Fatalf("canceled update persisted: value=%#v err=%v", unchanged, err)
	}
}

func TestFinanceRepositoryDatabaseFailureReturnsError(t *testing.T) {
	db := integrationDB(t)
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repository.NewAccountRepository(db).WithContext(context.Background()).FindByUserID(uuid.New()); err == nil || errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("database failure returned misleading result: %v", err)
	}
}
