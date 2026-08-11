package dto

import (
	"github.com/google/uuid"
)

type CreateBudgetRequest struct {
	CategoryID uuid.UUID `json:"category_id" binding:"required"`

	AllocatedAmount int64 `json:"allocated_amount" binding:"required,gt=0"`
}

type UpdateBudgetRequest struct {
	CategoryID uuid.UUID `json:"category_id" binding:"required"`

	AllocatedAmount int64 `json:"allocated_amount" binding:"required,gt=0"`
}

type BudgetResponse struct {
	ID         uuid.UUID `json:"id"`
	CategoryID uuid.UUID `json:"category_id"`

	AllocatedAmount int64 `json:"allocated_amount"`
}
