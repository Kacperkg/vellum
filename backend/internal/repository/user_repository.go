package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type UserRepository interface {
	Create(user *models.User) error
	CreateRegistration(user *models.User, settings *models.UserSettings, refreshToken *models.RefreshToken) error
	FindByIDContext(context.Context, uuid.UUID) (*models.User, error)
	FindByID(id uuid.UUID) (*models.User, error)
	FindByEmail(email string) (*models.User, error)
}
