package repositories

import (
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type GormBudgetRepository struct {
	db *gorm.DB
}

func NewGormBudgetRepository(db *gorm.DB) *GormBudgetRepository {
	return &GormBudgetRepository{
		db: db,
	}
}

func (r *GormBudgetRepository) Create(budget *models.Budget) error {
	return r.db.Create(budget).Error
}

func (r *GormBudgetRepository) FindByID(id uuid.UUID) (*models.Budget, error) {
	var budget models.Budget
	err := r.db.First(&budget, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &budget, nil
}

func (r *GormBudgetRepository) FindByUserID(userID uuid.UUID) ([]*models.Budget, error) {
	var budgets []*models.Budget
	err := r.db.Where("user_id = ?", userID).Find(&budgets).Error
	if err != nil {
		return nil, err
	}
	return budgets, nil
}

func (r *GormBudgetRepository) Update(userID uuid.UUID, budget *models.Budget) error {
	result := r.db.Model(&models.Budget{}).
		Where("id = ? AND user_id = ?", budget.ID, userID).
		Updates(map[string]any{
			"category_id":      budget.CategoryID,
			"allocated_amount": budget.AllocatedAmount,
		})
	return mutationError(result)
}

func (r *GormBudgetRepository) Delete(userID, id uuid.UUID) error {
	return mutationError(r.db.Delete(&models.Budget{}, "id = ? AND user_id = ?", id, userID))
}
