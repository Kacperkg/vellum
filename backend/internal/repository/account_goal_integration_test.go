package repository_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
)

func TestAccountGoalAllocationAndLedgerSummary(t *testing.T) {
	db := integrationDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	t.Cleanup(func() { tx.Rollback() })
	user := &models.User{FirstName: "Allocation", LastName: "Test", Email: uuid.NewString() + "@example.test", PasswordHash: "hash"}
	if err := tx.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	savings := &models.Account{UserID: user.ID, Name: "Savings"}
	bills := &models.Account{UserID: user.ID, Name: "Bills"}
	if err := tx.Create([]*models.Account{savings, bills}).Error; err != nil {
		t.Fatal(err)
	}
	category := &models.Category{Name: uuid.NewString()}
	if err := tx.Create(category).Error; err != nil {
		t.Fatal(err)
	}
	for _, record := range []*models.Transaction{
		{UserID: user.ID, Name: "Pay", Amount: 350000, AccountID: savings.ID, CategoryID: category.ID, Type: models.Income, Frequency: models.Monthly, Date: time.Now()},
		{UserID: user.ID, Name: "Spend", Amount: 50000, AccountID: savings.ID, CategoryID: category.ID, Type: models.Expense, Frequency: models.OneOff, Date: time.Now()},
	} {
		if err := tx.Create(record).Error; err != nil {
			t.Fatal(err)
		}
	}
	goals := repository.NewGoalRepository(tx)
	goalA := &models.Goal{UserID: user.ID, AccountID: &savings.ID, Name: "Trip", TargetAmount: 120000, SavedAmount: 10000}
	goalB := &models.Goal{UserID: user.ID, AccountID: &savings.ID, Name: "Computer", TargetAmount: 120000, SavedAmount: 20000}
	for _, goal := range []*models.Goal{goalA, goalB} {
		if err := goals.Create(goal); err != nil {
			t.Fatal(err)
		}
	}
	accounts := repository.NewAccountRepository(tx)
	service := services.NewAccountService(accounts)
	check := func(id uuid.UUID, balance, allocated, count int64) {
		t.Helper()
		a, err := service.GetByID(user.ID, id)
		if err != nil || a.Balance != balance || a.AllocatedAmount != allocated || a.UnallocatedAmount != balance-allocated || a.GoalCount != count {
			t.Fatalf("account=%#v want balance%d allocated%d count%d err=%v", a, balance, allocated, count, err)
		}
	}
	check(savings.ID, 300000, 240000, 2)
	check(bills.ID, 0, 0, 0)
	listed, err := service.FindByUserID(user.ID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("list=%#v err=%v", listed, err)
	}
	foreign, err := service.FindByUserID(uuid.New())
	if err != nil || len(foreign) != 0 {
		t.Fatalf("foreign=%#v err=%v", foreign, err)
	}
	transfers := repository.NewTransferRepository(tx)
	transfer := &models.Transfer{UserID: user.ID, Name: "Bills funding", Amount: 50000, FromAccountID: savings.ID, ToAccountID: bills.ID, Frequency: models.Monthly, Date: time.Now()}
	if err := transfers.Create(transfer); err != nil {
		t.Fatal(err)
	}
	check(savings.ID, 250000, 240000, 2)
	check(bills.ID, 50000, 0, 0)
	goalA.AccountID = &bills.ID
	if err := goals.Update(user.ID, goalA); err != nil {
		t.Fatal(err)
	}
	persisted, err := goals.FindByID(goalA.ID)
	if err != nil || persisted.AccountID == nil || *persisted.AccountID != bills.ID {
		t.Fatalf("reassigned=%#v err=%v", persisted, err)
	}
	check(savings.ID, 250000, 120000, 1)
	check(bills.ID, 50000, 120000, 1)
	goalA.AccountID = nil
	if err := goals.Update(user.ID, goalA); err != nil {
		t.Fatal(err)
	}
	persisted, err = goals.FindByID(goalA.ID)
	if err != nil || persisted.AccountID != nil {
		t.Fatalf("unlinked=%#v err=%v", persisted, err)
	}
	check(bills.ID, 50000, 0, 0)
	if err := transfers.Delete(user.ID, transfer.ID); err != nil {
		t.Fatal(err)
	}
	check(savings.ID, 300000, 120000, 1)
	check(bills.ID, 0, 0, 0)
	// The goal foreign key alone must protect an account, even without activity.
	goalB.AccountID = &bills.ID
	if err := goals.Update(user.ID, goalB); err != nil {
		t.Fatal(err)
	}
	if err := tx.SavePoint("goal_delete_guard").Error; err != nil {
		t.Fatal(err)
	}
	if err := accounts.Delete(user.ID, bills.ID); !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("linked account deletion err=%v", err)
	}
	if err := tx.RollbackTo("goal_delete_guard").Error; err != nil {
		t.Fatal(err)
	}
	goalB.AccountID = nil
	if err := goals.Update(user.ID, goalB); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Delete(user.ID, bills.ID); err != nil {
		t.Fatalf("unlinked account delete: %v", err)
	}
}
