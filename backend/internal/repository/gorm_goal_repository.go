package repository

import (
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"gorm.io/gorm"
)

type goalRepository struct {
	db *gorm.DB
}

func NewGoalRepository(db *gorm.DB) GoalRepository {
	return &goalRepository{
		db: db,
	}
}

func (r *goalRepository) Create(goal *models.Goal) error {
	if !money.Valid(goal.TargetAmount) {
		return money.ErrRange
	}
	if !money.Valid(goal.SavedAmount) {
		return money.ErrRange
	}
	if !money.Valid(goal.MonthlyContribution) {
		return money.ErrRange
	}
	return financialMutation(r.db, goal.UserID, func(tx *gorm.DB) error { return tx.Create(goal).Error })
}

func (r *goalRepository) FindByID(id uuid.UUID) (*models.Goal, error) {
	var goal models.Goal
	err := r.db.First(&goal, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &goal, nil
}

func (r *goalRepository) FindByUserID(userID uuid.UUID) ([]*models.Goal, error) {
	var goals []*models.Goal
	err := r.db.Where("user_id = ?", userID).Find(&goals).Error
	if err != nil {
		return nil, err
	}
	return goals, nil
}

func (r *goalRepository) Update(userID uuid.UUID, goal *models.Goal) error {
	if !money.Valid(goal.TargetAmount) {
		return money.ErrRange
	}
	if !money.Valid(goal.SavedAmount) {
		return money.ErrRange
	}
	if !money.Valid(goal.MonthlyContribution) {
		return money.ErrRange
	}
	return financialMutation(r.db, userID, func(tx *gorm.DB) error {
		result := tx.Model(&models.Goal{}).
			Where("id = ? AND user_id = ?", goal.ID, userID).
			Updates(map[string]any{
				"account_id":           goal.AccountID,
				"name":                 goal.Name,
				"target_amount":        goal.TargetAmount,
				"saved_amount":         goal.SavedAmount,
				"monthly_contribution": goal.MonthlyContribution,
			})
		return mutationError(result)

	})
}

func (r *goalRepository) Delete(userID, id uuid.UUID) error {
	return financialMutation(r.db, userID, func(tx *gorm.DB) error {
		return mutationError(tx.Delete(&models.Goal{}, "id = ? AND user_id = ?", id, userID))

	})
}
