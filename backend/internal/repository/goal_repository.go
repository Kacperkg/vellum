package repository

import (
	"context"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type GoalRepository interface {
	WithContext(context.Context) GoalRepository
	Create(goal *models.Goal) error
	FindByID(id uuid.UUID) (*models.Goal, error)
	FindByUserID(userID uuid.UUID) ([]*models.Goal, error)
	Update(userID uuid.UUID, goal *models.Goal) error
	Delete(userID, id uuid.UUID) error
}
