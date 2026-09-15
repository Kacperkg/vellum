package services

import (
	"github.com/google/uuid"
)

type CreateBudgetRequest struct {
	CategoryID      uuid.UUID
	AllocatedAmount int64
}

type UpdateBudgetRequest struct {
	CategoryID      uuid.UUID
	AllocatedAmount int64
}

type BudgetResponse struct {
	ID         uuid.UUID `json:"id"`
	CategoryID uuid.UUID `json:"category_id"`

	AllocatedAmount int64 `json:"allocated_amount"`
}
