package repository

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type UserRepository interface {
	Create(user *models.User) error
	CreateRegistration(user *models.User, settings *models.UserSettings, refreshToken *models.RefreshToken) error
	FindByID(id uuid.UUID) (*models.User, error)
	FindByEmail(email string) (*models.User, error)
}
