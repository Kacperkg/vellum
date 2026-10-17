package services

import (
	"github.com/google/uuid"
)

type CreateGoalRequest struct {
	AccountID           *uuid.UUID
	Name                string
	TargetAmount        int64
	SavedAmount         int64
	MonthlyContribution int64
}

type UpdateGoalRequest struct {
	AccountID           *uuid.UUID
	Name                string
	TargetAmount        int64
	SavedAmount         int64
	MonthlyContribution int64
}

type GoalResponse struct {
	AccountID           *uuid.UUID `json:"account_id"`
	ID                  uuid.UUID  `json:"id"`
	Name                string     `json:"name"`
	TargetAmount        int64      `json:"target_amount"`
	SavedAmount         int64      `json:"saved_amount"`
	MonthlyContribution int64      `json:"monthly_contribution"`
}
