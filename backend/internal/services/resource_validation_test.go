package services_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
)

func TestFinanceNameBounds(t *testing.T) {
	for _, test := range []struct{ label, name string }{
		{"ascii", strings.Repeat("a", 201)},
		{"unicode", strings.Repeat("界", 201)},
		{"whitespace", strings.Repeat(" ", 201)},
	} {
		t.Run(test.label, func(t *testing.T) {
			name := test.name
			userID, accountID, goalID, txID, transferID, destinationID, categoryID := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
			accounts := &fakeAccountRepository{Account: &models.Account{BaseModel: models.BaseModel{ID: accountID}, UserID: userID}}
			accountService := services.NewAccountService(accounts)
			goals := &fakeGoalRepository{Goal: &models.Goal{BaseModel: models.BaseModel{ID: goalID}, UserID: userID}}
			goalService := services.NewGoalService(goals, accounts)
			transactions := &fakeTransactionRepository{Transaction: &models.Transaction{BaseModel: models.BaseModel{ID: txID}, UserID: userID}}
			transactionService := services.NewTransactionService(transactions, accounts, &fakeCategoryRepository{})
			transfers := &fakeTransferRepository{transfer: &models.Transfer{BaseModel: models.BaseModel{ID: transferID}, UserID: userID}}
			transferService := services.NewTransferService(transfers, accounts)
			transactionRequest := services.CreateTransactionRequest{Name: name, Amount: 1, AccountID: accountID, CategoryID: categoryID, Date: time.Now(), Frequency: models.OneOff, Type: models.Income}
			transferRequest := services.CreateTransferRequest{Name: name, Amount: 1, FromAccountID: accountID, ToAccountID: destinationID, Date: time.Now(), Frequency: models.OneOff}
			checks := []func() error{
				func() error {
					_, err := accountService.Create(userID, services.CreateAccountRequest{Name: name})
					return err
				},
				func() error {
					_, err := accountService.Update(userID, accountID, services.UpdateAccountRequest{Name: name})
					return err
				},
				func() error {
					_, err := goalService.Create(userID, services.CreateGoalRequest{Name: name, TargetAmount: 1})
					return err
				},
				func() error {
					_, err := goalService.Update(userID, goalID, services.UpdateGoalRequest{Name: name, TargetAmount: 1})
					return err
				},
				func() error { _, err := transactionService.Create(userID, transactionRequest); return err },
				func() error {
					_, err := transactionService.Update(userID, txID, services.UpdateTransactionRequest(transactionRequest))
					return err
				},
				func() error { _, err := transferService.Create(userID, transferRequest); return err },
				func() error { _, err := transferService.Update(userID, transferID, transferRequest); return err },
			}
			for i, check := range checks {
				if err := check(); !errors.Is(err, services.ErrResourceNameTooLong) || !errors.Is(err, services.ErrInvalidInput) {
					t.Fatalf("check %d: error=%v", i, err)
				}
			}
			if accounts.Created != nil || accounts.Updated != nil || goals.Created != nil || goals.Updated != nil || transactions.Created != nil || transactions.Updated != nil || transfers.created != nil || transfers.updated != nil {
				t.Fatal("oversized name reached a financial mutation")
			}
		})
	}
	name := strings.Repeat("界", services.MaxResourceNameLength)
	response, err := services.NewAccountService(&fakeAccountRepository{}).Create(uuid.New(), services.CreateAccountRequest{Name: name})
	if err != nil || response.Name != name {
		t.Fatalf("boundary name was rejected or truncated: response=%#v err=%v", response, err)
	}
}

func TestTransactionDateRangeBounds(t *testing.T) {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	repo := &fakeTransactionRepository{Transactions: []*models.Transaction{{Amount: 42}}}
	service := services.NewTransactionService(repo, &fakeAccountRepository{}, &fakeCategoryRepository{})
	for _, end := range []time.Time{start, start.Add(services.MaxTransactionDateRange)} {
		rows, err := service.FindByDateRange(uuid.New(), start, end)
		if err != nil || len(rows) != 1 || rows[0].Amount != 42 {
			t.Fatalf("boundary range: rows=%#v err=%v", rows, err)
		}
	}
	for _, end := range []time.Time{start.Add(-time.Nanosecond), start.Add(services.MaxTransactionDateRange + time.Nanosecond), start.AddDate(100, 0, 0)} {
		if _, err := service.FindByDateRange(uuid.New(), start, end); !errors.Is(err, services.ErrTransactionDateRange) {
			t.Fatalf("invalid range end=%v: %v", end, err)
		}
	}
}
