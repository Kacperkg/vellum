package services

import (
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type CreateTransactionRequest struct {
	Name       string
	Amount     int64
	AccountID  uuid.UUID
	CategoryID uuid.UUID
	Date       time.Time
	Frequency  models.Frequency
	Type       models.TransactionType
}

type UpdateTransactionRequest struct {
	Name       string
	Amount     int64
	AccountID  uuid.UUID
	CategoryID uuid.UUID
	Date       time.Time
	Frequency  models.Frequency
	Type       models.TransactionType
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
