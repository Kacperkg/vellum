package repository

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type RefreshTokenRepository interface {
	Create(token *models.RefreshToken) error
	FindValid(tokenHash string, now time.Time) (*models.RefreshToken, error)
	Rotate(currentID uuid.UUID, replacement *models.RefreshToken, now time.Time) error
	RevokeByHash(tokenHash string, now time.Time) error
}

type refreshTokenRepository struct {
	db *gorm.DB
}

func NewRefreshTokenRepository(db *gorm.DB) RefreshTokenRepository {
	return &refreshTokenRepository{db: db}
}

func (r *refreshTokenRepository) Create(token *models.RefreshToken) error {
	return r.db.Create(token).Error
}

func (r *refreshTokenRepository) FindValid(tokenHash string, now time.Time) (*models.RefreshToken, error) {
	var token models.RefreshToken
	err := r.db.
		Where("token_hash = ? AND revoked_at IS NULL AND expires_at > ?", tokenHash, now).
		First(&token).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}

	return &token, nil
}

func (r *refreshTokenRepository) Rotate(currentID uuid.UUID, replacement *models.RefreshToken, now time.Time) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&models.RefreshToken{}).
			Where("id = ? AND revoked_at IS NULL AND expires_at > ?", currentID, now).
			Update("revoked_at", now)
		if err := mutationError(result); err != nil {
			return err
		}

		return tx.Create(replacement).Error
	})
}

func (r *refreshTokenRepository) RevokeByHash(tokenHash string, now time.Time) error {
	return r.db.Model(&models.RefreshToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", now).Error
}
