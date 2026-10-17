package services

import (
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type CreateTransferRequest struct {
	Name          string
	Amount        int64
	FromAccountID uuid.UUID
	ToAccountID   uuid.UUID
	Date          time.Time
	Frequency     models.Frequency
}

type UpdateTransferRequest = CreateTransferRequest

type TransferResponse struct {
	ID            uuid.UUID        `json:"id"`
	Name          string           `json:"name"`
	Amount        int64            `json:"amount"`
	FromAccountID uuid.UUID        `json:"from_account_id"`
	ToAccountID   uuid.UUID        `json:"to_account_id"`
	Date          time.Time        `json:"date"`
	Frequency     models.Frequency `json:"frequency"`
}
