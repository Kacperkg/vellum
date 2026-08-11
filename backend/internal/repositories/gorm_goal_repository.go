package repositories

import (
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type GormGoalRepository struct {
	db *gorm.DB
}

func NewGormGoalRepository(db *gorm.DB) *GormGoalRepository {
	return &GormGoalRepository{
		db: db,
	}
}

func (r *GormGoalRepository) Create(goal *models.Goal) error {
	return r.db.Create(goal).Error
}

func (r *GormGoalRepository) FindByID(id uuid.UUID) (*models.Goal, error) {
	var goal models.Goal
	err := r.db.First(&goal, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &goal, nil
}

func (r *GormGoalRepository) FindByUserID(userID uuid.UUID) ([]*models.Goal, error) {
	var goals []*models.Goal
	err := r.db.Where("user_id = ?", userID).Find(&goals).Error
	if err != nil {
		return nil, err
	}
	return goals, nil
}

func (r *GormGoalRepository) Update(userID uuid.UUID, goal *models.Goal) error {
	result := r.db.Model(&models.Goal{}).
		Where("id = ? AND user_id = ?", goal.ID, userID).
		Updates(map[string]any{
			"name":                 goal.Name,
			"target_amount":        goal.TargetAmount,
			"saved_amount":         goal.SavedAmount,
			"monthly_contribution": goal.MonthlyContribution,
		})
	return mutationError(result)
}

func (r *GormGoalRepository) Delete(userID, id uuid.UUID) error {
	return mutationError(r.db.Delete(&models.Goal{}, "id = ? AND user_id = ?", id, userID))
}
