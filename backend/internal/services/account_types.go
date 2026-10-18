package services

import (
	"github.com/google/uuid"
)

type CreateAccountRequest struct {
	Name string
}

type UpdateAccountRequest struct {
	Name string
}

type AccountResponse struct {
	ID                uuid.UUID `json:"id"`
	Name              string    `json:"name"`
	Balance           int64     `json:"balance"`
	AllocatedAmount   int64     `json:"allocated_amount"`
	UnallocatedAmount int64     `json:"unallocated_amount"`
	GoalCount         int64     `json:"goal_count"`
}
