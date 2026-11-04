package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type userSettingsRepository struct {
	db *gorm.DB
}

func NewUserSettingsRepository(db *gorm.DB) UserSettingsRepository {
	return &userSettingsRepository{
		db: db,
	}
}

func (r *userSettingsRepository) FindByUserID(userID uuid.UUID) (*models.UserSettings, error) {
	var settings models.UserSettings
	err := r.db.First(&settings, "user_id = ?", userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &settings, nil
}

func (r *userSettingsRepository) Update(userID uuid.UUID, settings *models.UserSettings) error {
	result := r.db.Model(&models.UserSettings{}).
		Where("id = ? AND user_id = ?", settings.ID, userID).
		Updates(map[string]any{
			"currency":       settings.Currency,
			"date_format":    settings.DateFormat,
			"number_format":  settings.NumberFormat,
			"week_starts_on": settings.WeekStartsOn,
		})
	return mutationError(result)
}

func (r *userSettingsRepository) Create(settings *models.UserSettings) error {
	return r.db.Create(settings).Error
}

// WithContext returns a request-scoped clone; the shared repository is never mutated.
func (r *userSettingsRepository) WithContext(ctx context.Context) UserSettingsRepository {
	return &userSettingsRepository{db: r.db.WithContext(ctx)}
}
