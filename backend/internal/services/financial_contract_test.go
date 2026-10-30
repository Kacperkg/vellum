package services_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/apperrors"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"github.com/kacperkg/vellum/internal/services"
)

func TestGoalAndBudgetMoneyBounds(t *testing.T) {
	userID, categoryID := uuid.New(), uuid.New()
	goalRepo := &fakeGoalRepository{}
	goals := services.NewGoalService(goalRepo, &fakeAccountRepository{})
	for _, req := range []services.CreateGoalRequest{
		{Name: "Target", TargetAmount: money.Max + 1},
		{Name: "Saved", TargetAmount: money.Max, SavedAmount: money.Max + 1},
		{Name: "Contribution", TargetAmount: 1, MonthlyContribution: money.Max + 1},
	} {
		if _, err := goals.Create(userID, req); !errors.Is(err, apperrors.ErrInvalidInput) {
			t.Fatalf("goal=%v: %v", req, err)
		}
		if goalRepo.Created != nil {
			t.Fatal("unsupported goal persisted")
		}
	}
	for _, req := range []services.CreateGoalRequest{{Name: "Minimum", TargetAmount: 1}, {Name: "Maximum", TargetAmount: money.Max, SavedAmount: money.Max, MonthlyContribution: money.Max}} {
		if _, err := goals.Create(userID, req); err != nil {
			t.Fatal(err)
		}
	}
	budgetRepo := &fakeBudgetRepository{}
	budgets := services.NewBudgetService(budgetRepo, &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}}})
	if _, err := budgets.Create(userID, services.CreateBudgetRequest{CategoryID: categoryID, AllocatedAmount: money.Max + 1}); !errors.Is(err, apperrors.ErrInvalidInput) {
		t.Fatal(err)
	}
	if budgetRepo.Created != nil {
		t.Fatal("unsupported budget persisted")
	}
	for _, amount := range []int64{1, money.Max} {
		if _, err := budgets.Create(userID, services.CreateBudgetRequest{CategoryID: categoryID, AllocatedAmount: amount}); err != nil {
			t.Fatal(err)
		}
	}
}
