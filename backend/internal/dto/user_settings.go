package dto

import (
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type UpdateUserSettingsRequest struct {
	Currency     models.Currency     `json:"currency" binding:"required,oneof=GBP USD EUR"`
	DateFormat   models.DateFormat   `json:"date_format" binding:"required,oneof='DD/MM/YYYY' 'MM/DD/YYYY' 'YYYY-MM-DD'"`
	NumberFormat models.NumberFormat `json:"number_format" binding:"required,oneof=comma_decimal dot_decimal apostrophe"`
	WeekStartsOn models.WeekStartsOn `json:"week_starts_on" binding:"required,oneof=Monday Sunday"`
}

type UserSettingsResponse struct {
	ID           uuid.UUID           `json:"id"`
	Currency     models.Currency     `json:"currency"`
	DateFormat   models.DateFormat   `json:"date_format"`
	NumberFormat models.NumberFormat `json:"number_format"`
	WeekStartsOn models.WeekStartsOn `json:"week_starts_on"`
}
