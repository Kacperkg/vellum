package repositories

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type UserRepository interface {
	Create(user *models.User) error
	FindByID(id uuid.UUID) (*models.User, error)
	FindByEmail(email string) (*models.User, error)
}
