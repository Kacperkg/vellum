package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
)

func TestTransferRepositoryAtomicLedgerAndIntegrity(t *testing.T) {
	db := integrationDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	user := &models.User{FirstName: "Transfer", LastName: "Test", Email: uuid.NewString() + "@example.test", PasswordHash: "hash"}
	if err := tx.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	from := &models.Account{UserID: user.ID, Name: "Salary"}
	to := &models.Account{UserID: user.ID, Name: "Subscriptions"}
	if err := tx.Create([]*models.Account{from, to}).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.NewTransferRepository(tx)
	transfer := &models.Transfer{UserID: user.ID, Name: "Funding", Amount: 12345, FromAccountID: from.ID, ToAccountID: to.ID, Date: time.Now()}
	if err := repo.Create(transfer); err != nil {
		t.Fatal(err)
	}
	listed, err := repo.FindByUserID(user.ID)
	if err != nil || len(listed) != 1 || listed[0].Amount != 12345 || listed[0].Frequency != models.OneOff {
		t.Fatalf("listed=%#v error=%v", listed, err)
	}
	foreign, err := repo.FindByUserID(uuid.New())
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign=%#v error=%v", foreign, err)
	}
	var transactionCount int64
	if err := tx.Model(&models.Transaction{}).Where("user_id = ?", user.ID).Count(&transactionCount).Error; err != nil || transactionCount != 0 {
		t.Fatalf("income/expense rows=%d error=%v", transactionCount, err)
	}

	transfer.Amount = 25000
	transfer.Frequency = models.Monthly
	transfer.FromAccountID, transfer.ToAccountID = to.ID, from.ID
	if err := repo.Update(uuid.New(), transfer); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("foreign update error=%v", err)
	}
	unchanged, err := repo.FindByID(transfer.ID)
	if err != nil || unchanged.Amount != 12345 || unchanged.FromAccountID != from.ID || unchanged.ToAccountID != to.ID {
		t.Fatalf("foreign mutation=%#v error=%v", unchanged, err)
	}
	if err := repo.Update(user.ID, transfer); err != nil {
		t.Fatal(err)
	}
	updated, err := repo.FindByID(transfer.ID)
	if err != nil || updated.Amount != 25000 || updated.FromAccountID != to.ID || updated.ToAccountID != from.ID || updated.Frequency != models.Monthly {
		t.Fatalf("updated=%#v error=%v", updated, err)
	}
	for _, frequency := range []models.Frequency{models.Daily, models.Weekly, models.Monthly, models.Yearly, models.OneOff} {
		transfer.Frequency = frequency
		if err := repo.Update(user.ID, transfer); err != nil {
			t.Fatal(err)
		}
		persisted, err := repo.FindByID(transfer.ID)
		if err != nil || persisted.Frequency != frequency || persisted.Amount != 25000 || persisted.FromAccountID != to.ID || persisted.ToAccountID != from.ID {
			t.Fatalf("frequency roundtrip=%#v error=%v", persisted, err)
		}
	}
	if err := tx.Model(&models.Transaction{}).Where("user_id = ?", user.ID).Count(&transactionCount).Error; err != nil || transactionCount != 0 {
		t.Fatalf("recurring transfers created income/expense rows=%d error=%v", transactionCount, err)
	}

	accounts := repository.NewAccountRepository(tx)
	for _, id := range []uuid.UUID{from.ID, to.ID} {
		if err := tx.SavePoint("before_account_delete").Error; err != nil {
			t.Fatal(err)
		}
		if err := accounts.Delete(user.ID, id); !errors.Is(err, repository.ErrConflict) {
			t.Fatalf("referenced account delete error=%v", err)
		}
		if err := tx.RollbackTo("before_account_delete").Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, invalid := range []*models.Transfer{
		{UserID: user.ID, Name: "Invalid amount", Amount: 0, FromAccountID: from.ID, ToAccountID: to.ID, Date: time.Now()},
		{UserID: user.ID, Name: "Same account", Amount: 100, FromAccountID: from.ID, ToAccountID: from.ID, Date: time.Now()},
		{UserID: user.ID, Name: "Invalid frequency", Amount: 100, FromAccountID: from.ID, ToAccountID: to.ID, Date: time.Now(), Frequency: "fortnightly"},
	} {
		if err := tx.SavePoint("before_invalid_transfer").Error; err != nil {
			t.Fatal(err)
		}
		if err := repo.Create(invalid); err == nil {
			t.Fatalf("invalid transfer persisted: %#v", invalid)
		}
		if err := tx.RollbackTo("before_invalid_transfer").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := repo.Delete(uuid.New(), transfer.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("foreign delete error=%v", err)
	}
	if err := repo.Delete(user.ID, transfer.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(transfer.ID); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("deleted transfer error=%v", err)
	}
	if err := accounts.Delete(user.ID, from.ID); err != nil {
		t.Fatalf("unreferenced account delete: %v", err)
	}
	if err := accounts.Delete(user.ID, to.ID); err != nil {
		t.Fatalf("unreferenced destination delete: %v", err)
	}
}
