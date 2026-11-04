package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type categoryRepository struct {
	db *gorm.DB
}

func NewCategoryRepository(db *gorm.DB) CategoryRepository {
	return &categoryRepository{
		db: db,
	}
}

func (r *categoryRepository) FindByID(id uuid.UUID) (*models.Category, error) {
	var category models.Category
	err := r.db.First(&category, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &category, nil
}

func (r *categoryRepository) ListAll() ([]*models.Category, error) {
	var categories []*models.Category
	err := r.db.Find(&categories).Error
	if err != nil {
		return nil, err
	}
	return categories, nil
}

// WithContext returns a request-scoped clone; the shared repository is never mutated.
func (r *categoryRepository) WithContext(ctx context.Context) CategoryRepository {
	return &categoryRepository{db: r.db.WithContext(ctx)}
}
