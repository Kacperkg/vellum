package repositories

import (
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type GormUserSettingsRepository struct {
	db *gorm.DB
}

func NewGormUserSettingsRepository(db *gorm.DB) *GormUserSettingsRepository {
	return &GormUserSettingsRepository{
		db: db,
	}
}

func (r *GormUserSettingsRepository) FindByUserID(userID uuid.UUID) (*models.UserSettings, error) {
	var settings models.UserSettings
	err := r.db.First(&settings, "user_id = ?", userID).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &settings, nil
}

func (r *GormUserSettingsRepository) Update(userID uuid.UUID, settings *models.UserSettings) error {
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

func (r *GormUserSettingsRepository) Create(settings *models.UserSettings) error {
	return r.db.Create(settings).Error
}
