package services_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
)

type conflictingBudgetWrites struct{ *fakeBudgetRepository }

func (r conflictingBudgetWrites) Create(*models.Budget) error { return repository.ErrConflict }
func (r conflictingBudgetWrites) Update(uuid.UUID, *models.Budget) error {
	return repository.ErrConflict
}

func TestBudgetServiceMapsDatabaseUniquenessConflicts(t *testing.T) {
	userID, budgetID, categoryID := uuid.New(), uuid.New(), uuid.New()
	repo := conflictingBudgetWrites{&fakeBudgetRepository{Budget: &models.Budget{BaseModel: models.BaseModel{ID: budgetID}, UserID: userID}}}
	category := &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}}}
	service := services.NewBudgetService(repo, category)
	_, createErr := service.Create(userID, services.CreateBudgetRequest{CategoryID: categoryID, AllocatedAmount: 10000})
	_, updateErr := service.Update(userID, budgetID, services.UpdateBudgetRequest{CategoryID: categoryID, AllocatedAmount: 10000})
	for _, err := range []error{createErr, updateErr} {
		if !errors.Is(err, services.ErrBudgetCategoryExists) || !errors.Is(err, services.ErrConflict) {
			t.Fatalf("database conflict mapped to %v", err)
		}
	}
}
