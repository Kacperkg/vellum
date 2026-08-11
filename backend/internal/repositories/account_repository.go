package repositories

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type AccountRepository interface {
	Create(account *models.Account) error
	GetByID(id uuid.UUID) (*models.Account, error)
	FindByUserID(userID uuid.UUID) ([]*models.Account, error)
	Update(userID uuid.UUID, account *models.Account) error
	Delete(userID, id uuid.UUID) error
}
