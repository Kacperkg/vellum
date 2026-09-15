package repository

import (
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type TransactionRepository interface {
	Create(transaction *models.Transaction) error
	FindByID(id uuid.UUID) (*models.Transaction, error)
	FindByUserID(userID uuid.UUID) ([]*models.Transaction, error)
	Update(userID uuid.UUID, transaction *models.Transaction) error
	Delete(userID, id uuid.UUID) error
	FindByCategoryID(userId, categoryID uuid.UUID) ([]*models.Transaction, error)
	FindByDateRange(userID uuid.UUID, startDate, endDate time.Time) ([]*models.Transaction, error)
	FindByFrequency(userID uuid.UUID, frequency models.Frequency) ([]*models.Transaction, error)
	FindByAccountID(userID, accountID uuid.UUID) ([]*models.Transaction, error)
}
