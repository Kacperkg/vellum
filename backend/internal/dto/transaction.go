package dto

import (
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type CreateTransactionRequest struct {
	Name   string `json:"name" binding:"required"`
	Amount int64  `json:"amount" binding:"required,gt=0"`

	AccountID uuid.UUID `json:"account_id" binding:"required"`

	CategoryID uuid.UUID `json:"category_id" binding:"required"`

	Date time.Time `json:"date" binding:"required"`

	Frequency models.Frequency       `json:"frequency" binding:"required,oneof=one-off daily weekly monthly yearly"`
	Type      models.TransactionType `json:"type" binding:"required,oneof=income expense"`
}

type UpdateTransactionRequest struct {
	Name   string `json:"name" binding:"required"`
	Amount int64  `json:"amount" binding:"required,gt=0"`

	AccountID uuid.UUID `json:"account_id" binding:"required"`

	CategoryID uuid.UUID `json:"category_id" binding:"required"`

	Date time.Time `json:"date" binding:"required"`

	Frequency models.Frequency       `json:"frequency" binding:"required,oneof=one-off daily weekly monthly yearly"`
	Type      models.TransactionType `json:"type" binding:"required,oneof=income expense"`
}

type TransactionResponse struct {
	ID     uuid.UUID `json:"id"`
	Name   string    `json:"name"`
	Amount int64     `json:"amount"`

	AccountID uuid.UUID `json:"account_id"`

	CategoryID uuid.UUID `json:"category_id"`

	Date time.Time `json:"date"`

	Frequency models.Frequency       `json:"frequency"`
	Type      models.TransactionType `json:"type"`
}
