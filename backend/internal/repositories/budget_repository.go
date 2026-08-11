package repositories

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type BudgetRepository interface {
	FindByID(id uuid.UUID) (*models.Budget, error)
	FindByUserID(userID uuid.UUID) ([]*models.Budget, error)
	Create(budget *models.Budget) error
	Update(userID uuid.UUID, budget *models.Budget) error
	Delete(userID, id uuid.UUID) error
}
