package services_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
	"github.com/kacperkg/vellum/tests/testutil"
)

func TestGoalService(t *testing.T) {
	userID, goalID := uuid.New(), uuid.New()
	valid := dto.CreateGoalRequest{Name: " Emergency ", TargetAmount: 100000, SavedAmount: 25000, MonthlyContribution: 10000}

	t.Run("create", func(t *testing.T) {
		repo := &testutil.GoalRepository{}
		response, err := services.NewGoalService(repo).Create(userID, valid)
		if err != nil || repo.Created == nil || repo.Created.Name != "Emergency" || response.ID == uuid.Nil {
			t.Fatalf("response=%#v created=%#v err=%v", response, repo.Created, err)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(*dto.CreateGoalRequest)
		want   error
	}{
		{"name", func(r *dto.CreateGoalRequest) { r.Name = " " }, services.ErrGoalNameRequired},
		{"target", func(r *dto.CreateGoalRequest) { r.TargetAmount = 0 }, services.ErrGoalTargetPositive},
		{"saved negative", func(r *dto.CreateGoalRequest) { r.SavedAmount = -1 }, services.ErrGoalSavedAmountNegative},
		{"saved exceeds", func(r *dto.CreateGoalRequest) { r.SavedAmount = r.TargetAmount + 1 }, services.ErrGoalSavedExceedsTarget},
		{"contribution", func(r *dto.CreateGoalRequest) { r.MonthlyContribution = -1 }, services.ErrGoalContributionNegative},
	} {
		t.Run(test.name, func(t *testing.T) {
			req := valid
			test.mutate(&req)
			_, err := services.NewGoalService(&testutil.GoalRepository{}).Create(userID, req)
			if !errors.Is(err, test.want) {
				t.Fatalf("error=%v want=%v", err, test.want)
			}
		})
	}

	t.Run("zero contribution is allowed", func(t *testing.T) {
		req := valid
		req.MonthlyContribution = 0
		if _, err := services.NewGoalService(&testutil.GoalRepository{}).Create(userID, req); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ownership update delete and list", func(t *testing.T) {
		goal := &models.Goal{BaseModel: models.BaseModel{ID: goalID}, UserID: userID}
		repo := &testutil.GoalRepository{Goal: goal, Goals: []*models.Goal{goal}}
		service := services.NewGoalService(repo)
		update := dto.UpdateGoalRequest{Name: "Holiday", TargetAmount: 200000, SavedAmount: 50000, MonthlyContribution: 20000}
		if _, err := service.Update(userID, goalID, update); err != nil || repo.Updated == nil {
			t.Fatalf("updated=%#v err=%v", repo.Updated, err)
		}
		list, err := service.FindByUserID(userID)
		if err != nil || len(list) != 1 {
			t.Fatalf("list=%#v err=%v", list, err)
		}
		if err := service.Delete(userID, goalID); err != nil || repo.Deleted != goalID {
			t.Fatalf("deleted=%v err=%v", repo.Deleted, err)
		}
		goal.UserID = uuid.New()
		if _, err := service.GetByID(userID, goalID); !errors.Is(err, services.ErrGoalNotFound) {
			t.Fatalf("error=%v", err)
		}
	})
}

func transactionFixture(userID uuid.UUID) (*services.TransactionService, *testutil.TransactionRepository, uuid.UUID, uuid.UUID) {
	accountID, categoryID := uuid.New(), uuid.New()
	repo := &testutil.TransactionRepository{}
	service := services.NewTransactionService(repo, &testutil.AccountRepository{Account: &models.Account{BaseModel: models.BaseModel{ID: accountID}, UserID: userID}}, &testutil.CategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}}})
	return service, repo, accountID, categoryID
}

func transactionRequest(accountID, categoryID uuid.UUID) dto.CreateTransactionRequest {
	return dto.CreateTransactionRequest{Name: " Salary ", Amount: 250050, AccountID: accountID, CategoryID: categoryID, Date: time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC), Frequency: models.Monthly, Type: models.Income}
}

func TestTransactionService(t *testing.T) {
	userID := uuid.New()
	_, _, accountID, categoryID := transactionFixture(userID)
	valid := transactionRequest(accountID, categoryID)

	t.Run("create", func(t *testing.T) {
		service, repo, _, _ := transactionFixture(userID)
		response, err := service.Create(userID, valid)
		if err != nil || repo.Created == nil || repo.Created.Name != "Salary" || response.ID == uuid.Nil {
			t.Fatalf("response=%#v created=%#v err=%v", response, repo.Created, err)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(*dto.CreateTransactionRequest)
		want   error
	}{
		{"name", func(r *dto.CreateTransactionRequest) { r.Name = " " }, services.ErrTransactionNameRequired},
		{"amount", func(r *dto.CreateTransactionRequest) { r.Amount = 0 }, services.ErrTransactionAmountPositive},
		{"account", func(r *dto.CreateTransactionRequest) { r.AccountID = uuid.Nil }, services.ErrTransactionAccountRequired},
		{"category", func(r *dto.CreateTransactionRequest) { r.CategoryID = uuid.Nil }, services.ErrCategoryRequired},
		{"date", func(r *dto.CreateTransactionRequest) { r.Date = time.Time{} }, services.ErrTransactionDateRequired},
		{"frequency", func(r *dto.CreateTransactionRequest) { r.Frequency = "sometimes" }, services.ErrTransactionFrequency},
		{"type", func(r *dto.CreateTransactionRequest) { r.Type = "transfer" }, services.ErrTransactionType},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, repo, _, _ := transactionFixture(userID)
			req := valid
			test.mutate(&req)
			_, err := service.Create(userID, req)
			if !errors.Is(err, test.want) || repo.Created != nil {
				t.Fatalf("created=%#v error=%v want=%v", repo.Created, err, test.want)
			}
		})
	}

	t.Run("rejects foreign account", func(t *testing.T) {
		service := services.NewTransactionService(&testutil.TransactionRepository{}, &testutil.AccountRepository{Account: &models.Account{UserID: uuid.New()}}, &testutil.CategoryRepository{Category: &models.Category{}})
		_, err := service.Create(userID, valid)
		if !errors.Is(err, services.ErrAccountNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("ownership update delete and filters", func(t *testing.T) {
		service, repo, _, _ := transactionFixture(userID)
		id := uuid.New()
		tx := &models.Transaction{BaseModel: models.BaseModel{ID: id}, UserID: userID, AccountID: accountID, CategoryID: categoryID}
		repo.Transaction = tx
		repo.Transactions = []*models.Transaction{tx}
		update := dto.UpdateTransactionRequest(valid)
		if _, err := service.Update(userID, id, update); err != nil || repo.Updated == nil {
			t.Fatalf("updated=%#v err=%v", repo.Updated, err)
		}
		if list, err := service.FindByUserID(userID); err != nil || len(list) != 1 {
			t.Fatalf("list=%#v err=%v", list, err)
		}
		if list, err := service.FindByAccountID(userID, accountID); err != nil || len(list) != 1 {
			t.Fatalf("list=%#v err=%v", list, err)
		}
		if list, err := service.FindByCategoryID(userID, categoryID); err != nil || len(list) != 1 {
			t.Fatalf("list=%#v err=%v", list, err)
		}
		if list, err := service.FindByDateRange(userID, time.Now().Add(-time.Hour), time.Now()); err != nil || len(list) != 1 {
			t.Fatalf("list=%#v err=%v", list, err)
		}
		if list, err := service.FindByFrequency(userID, models.Monthly); err != nil || len(list) != 1 {
			t.Fatalf("list=%#v err=%v", list, err)
		}
		if err := service.Delete(userID, id); err != nil || repo.Deleted != id {
			t.Fatalf("deleted=%v err=%v", repo.Deleted, err)
		}
		tx.UserID = uuid.New()
		if _, err := service.GetByID(userID, id); !errors.Is(err, services.ErrTransactionNotFound) {
			t.Fatalf("error=%v", err)
		}
	})

	t.Run("filter validation", func(t *testing.T) {
		service, _, _, _ := transactionFixture(userID)
		if _, err := service.FindByDateRange(userID, time.Now(), time.Now().Add(-time.Hour)); !errors.Is(err, services.ErrTransactionDateRange) {
			t.Fatalf("error=%v", err)
		}
		if _, err := service.FindByFrequency(userID, "sometimes"); !errors.Is(err, services.ErrTransactionFrequency) {
			t.Fatalf("error=%v", err)
		}
	})
}
