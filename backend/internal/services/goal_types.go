package services

import (
	"github.com/google/uuid"
)

type CreateGoalRequest struct {
	Name                string
	TargetAmount        int64
	SavedAmount         int64
	MonthlyContribution int64
}

type UpdateGoalRequest struct {
	Name                string
	TargetAmount        int64
	SavedAmount         int64
	MonthlyContribution int64
}

type GoalResponse struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	TargetAmount        int64     `json:"target_amount"`
	SavedAmount         int64     `json:"saved_amount"`
	MonthlyContribution int64     `json:"monthly_contribution"`
}
