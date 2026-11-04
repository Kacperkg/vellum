package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type CategoryRepository interface {
	WithContext(context.Context) CategoryRepository
	FindByID(id uuid.UUID) (*models.Category, error)
	ListAll() ([]*models.Category, error)
}
