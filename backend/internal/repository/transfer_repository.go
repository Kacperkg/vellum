package repository

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type TransferRepository interface {
	Create(transfer *models.Transfer) error
	FindByID(id uuid.UUID) (*models.Transfer, error)
	FindByUserID(userID uuid.UUID) ([]*models.Transfer, error)
	Update(userID uuid.UUID, transfer *models.Transfer) error
	Delete(userID, id uuid.UUID) error
}
