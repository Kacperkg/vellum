package services_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
)

func TestGoalAccountOwnershipAndUnlink(t *testing.T) {
	userID, accountID, goalID := uuid.New(), uuid.New(), uuid.New()
	req := services.CreateGoalRequest{Name: "Holiday", TargetAmount: 120000, SavedAmount: 10000, AccountID: &accountID}
	for _, test := range []struct {
		name    string
		account *models.Account
		err     error
	}{
		{"owned", &models.Account{UserID: userID}, nil},
		{"foreign", &models.Account{UserID: uuid.New()}, services.ErrAccountNotFound},
		{"missing", nil, services.ErrAccountNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := &fakeGoalRepository{}
			service := services.NewGoalService(repo, &fakeAccountRepository{Account: test.account})
			response, err := service.Create(userID, req)
			if !errors.Is(err, test.err) {
				t.Fatalf("error=%v want=%v", err, test.err)
			}
			if err == nil && (repo.Created.AccountID == nil || *response.AccountID != accountID || response.SavedAmount != 10000) {
				t.Fatalf("response=%#v", response)
			}
			if err != nil && repo.Created != nil {
				t.Fatal("invalid link persisted")
			}
		})
	}
	repo := &fakeGoalRepository{Goal: &models.Goal{BaseModel: models.BaseModel{ID: goalID}, UserID: userID, AccountID: &accountID}}
	service := services.NewGoalService(repo, &fakeAccountRepository{Err: repository.ErrNotFound})
	update := services.UpdateGoalRequest{Name: "Holiday", TargetAmount: 120000, SavedAmount: 10000, AccountID: &accountID}
	if _, err := service.Update(userID, goalID, update); !errors.Is(err, services.ErrAccountNotFound) || repo.Updated != nil {
		t.Fatalf("foreign link update err=%v", err)
	}
	update.AccountID = nil
	response, err := service.Update(userID, goalID, update)
	if err != nil || response.AccountID != nil || repo.Updated.AccountID != nil {
		t.Fatalf("unlink response=%#v err=%v", response, err)
	}
	// Goal ownership is checked before account lookup; neither resource leaks.
	repo.Goal.UserID = uuid.New()
	update.AccountID = &accountID
	if _, err := service.Update(userID, goalID, update); !errors.Is(err, services.ErrGoalNotFound) {
		t.Fatalf("foreign goal error=%v", err)
	}
}

func TestAccountSummaryResponsePreservesShortfall(t *testing.T) {
	userID, accountID := uuid.New(), uuid.New()
	account := &models.Account{BaseModel: models.BaseModel{ID: accountID}, UserID: userID, Balance: 300000, AllocatedAmount: 240000, GoalCount: 2}
	service := services.NewAccountService(&fakeAccountRepository{Account: account, Accounts: []*models.Account{account}})
	response, err := service.GetByID(userID, accountID)
	if err != nil || response.UnallocatedAmount != 60000 || response.GoalCount != 2 {
		t.Fatalf("response=%#v err=%v", response, err)
	}
	account.Balance = 100000
	list, err := service.FindByUserID(userID)
	if err != nil || len(list) != 1 || list[0].UnallocatedAmount != -140000 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
}
