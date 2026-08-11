package services

import (
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repositories"
)

type UserSettingsService struct {
	userSettingsRepository repositories.UserSettingsRepository
}

func NewUserSettingsService(repository repositories.UserSettingsRepository) *UserSettingsService {
	return &UserSettingsService{userSettingsRepository: repository}
}

func (s *UserSettingsService) Get(userID uuid.UUID) (*dto.UserSettingsResponse, error) {
	settings, err := s.userSettingsRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, ErrSettingsNotFound
	}

	return userSettingsResponse(settings), nil
}

func (s *UserSettingsService) Update(userID uuid.UUID, req dto.UpdateUserSettingsRequest) (*dto.UserSettingsResponse, error) {
	if err := validateUserSettings(req); err != nil {
		return nil, err
	}

	settings, err := s.userSettingsRepository.FindByUserID(userID)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, ErrSettingsNotFound
	}

	settings.Currency = req.Currency
	settings.DateFormat = req.DateFormat
	settings.NumberFormat = req.NumberFormat
	settings.WeekStartsOn = req.WeekStartsOn

	if err := s.userSettingsRepository.Update(userID, settings); err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, ErrSettingsNotFound
		}
		return nil, err
	}

	return userSettingsResponse(settings), nil
}

func validateUserSettings(req dto.UpdateUserSettingsRequest) error {
	if req.Currency != models.GBP && req.Currency != models.USD && req.Currency != models.EUR {
		return ErrSettingsCurrencyInvalid
	}
	if req.DateFormat != models.DDMMYYYY && req.DateFormat != models.MMDDYYYY && req.DateFormat != models.YYYYMMDD {
		return ErrSettingsDateFormatInvalid
	}
	if req.NumberFormat != models.CommaDecimal && req.NumberFormat != models.DotDecimal && req.NumberFormat != models.Apostrophe {
		return ErrSettingsNumberFormatInvalid
	}
	if req.WeekStartsOn != models.Monday && req.WeekStartsOn != models.Sunday {
		return ErrSettingsWeekStartInvalid
	}
	return nil
}

func userSettingsResponse(settings *models.UserSettings) *dto.UserSettingsResponse {
	return &dto.UserSettingsResponse{
		ID:           settings.ID,
		Currency:     settings.Currency,
		DateFormat:   settings.DateFormat,
		NumberFormat: settings.NumberFormat,
		WeekStartsOn: settings.WeekStartsOn,
	}
}
