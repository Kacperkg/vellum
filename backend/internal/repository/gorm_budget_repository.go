package repository

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type budgetRepository struct {
	db *gorm.DB
}

func NewBudgetRepository(db *gorm.DB) BudgetRepository {
	return &budgetRepository{
		db: db,
	}
}

func (r *budgetRepository) Create(budget *models.Budget) error {
	return budgetWriteError(r.db.Create(budget).Error)
}

func (r *budgetRepository) FindByID(id uuid.UUID) (*models.Budget, error) {
	var budget models.Budget
	err := r.db.First(&budget, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &budget, nil
}

func (r *budgetRepository) FindByUserID(userID uuid.UUID) ([]*models.Budget, error) {
	var budgets []*models.Budget
	err := r.db.Where("user_id = ?", userID).Find(&budgets).Error
	if err != nil {
		return nil, err
	}
	return budgets, nil
}

func (r *budgetRepository) Update(userID uuid.UUID, budget *models.Budget) error {
	result := r.db.Model(&models.Budget{}).
		Where("id = ? AND user_id = ?", budget.ID, userID).
		Updates(map[string]any{
			"category_id":      budget.CategoryID,
			"allocated_amount": budget.AllocatedAmount,
		})
	if err := budgetWriteError(result.Error); err != nil {
		return err
	}
	return mutationError(result)
}

func budgetWriteError(err error) error {
	var constraintError *pgconn.PgError
	if errors.As(err, &constraintError) && constraintError.Code == "23505" && constraintError.ConstraintName == models.BudgetUserCategoryUniqueIndex {
		return ErrConflict
	}
	return err
}

func (r *budgetRepository) Delete(userID, id uuid.UUID) error {
	return mutationError(r.db.Delete(&models.Budget{}, "id = ? AND user_id = ?", id, userID))
}
