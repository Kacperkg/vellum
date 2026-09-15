package services

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type UpdateUserSettingsRequest struct {
	Currency     models.Currency
	DateFormat   models.DateFormat
	NumberFormat models.NumberFormat
	WeekStartsOn models.WeekStartsOn
}

type UserSettingsResponse struct {
	ID           uuid.UUID           `json:"id"`
	Currency     models.Currency     `json:"currency"`
	DateFormat   models.DateFormat   `json:"date_format"`
	NumberFormat models.NumberFormat `json:"number_format"`
	WeekStartsOn models.WeekStartsOn `json:"week_starts_on"`
}
