package dto

import (
	"github.com/google/uuid"
)

type CreateGoalRequest struct {
	Name                string `json:"name" binding:"required"`
	TargetAmount        int64  `json:"target_amount" binding:"required,gt=0"`
	SavedAmount         int64  `json:"saved_amount" binding:"gte=0"`
	MonthlyContribution int64  `json:"monthly_contribution" binding:"gte=0"`
}

type UpdateGoalRequest struct {
	Name                string `json:"name" binding:"required"`
	TargetAmount        int64  `json:"target_amount" binding:"required,gt=0"`
	SavedAmount         int64  `json:"saved_amount" binding:"gte=0"`
	MonthlyContribution int64  `json:"monthly_contribution" binding:"gte=0"`
}

type GoalResponse struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	TargetAmount        int64     `json:"target_amount"`
	SavedAmount         int64     `json:"saved_amount"`
	MonthlyContribution int64     `json:"monthly_contribution"`
}
