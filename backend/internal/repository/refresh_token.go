package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	ctx, cancel := context.WithTimeout(r.db.Statement.Context, 5*time.Second)
	defer cancel()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSessionUser(tx, token); err != nil {
			return err
		}
		return tx.Create(token).Error
	})
}

func (r *refreshTokenRepository) FindValid(tokenHash string, now time.Time) (*models.RefreshToken, error) {
	ctx, cancel := context.WithTimeout(r.db.Statement.Context, 5*time.Second)
	defer cancel()
	var token models.RefreshToken
	err := r.db.WithContext(ctx).
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
	ctx, cancel := context.WithTimeout(r.db.Statement.Context, 5*time.Second)
	defer cancel()
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockSessionUser(tx, replacement); err != nil {
			return err
		}
		now = time.Now()
		result := tx.Model(&models.RefreshToken{}).
			Where("id = ? AND user_id = ? AND auth_version = ? AND revoked_at IS NULL AND expires_at > ?", currentID, replacement.UserID, replacement.AuthVersion, now).
			Update("revoked_at", now)
		if err := mutationError(result); err != nil {
			return err
		}

		return tx.Create(replacement).Error
	})
}

func (r *refreshTokenRepository) RevokeByHash(tokenHash string, now time.Time) error {
	ctx, cancel := context.WithTimeout(r.db.Statement.Context, 5*time.Second)
	defer cancel()
	return r.db.WithContext(ctx).Model(&models.RefreshToken{}).
		Where("token_hash = ? AND revoked_at IS NULL", tokenHash).
		Update("revoked_at", now).Error
}

// All session writers take the same user lock as password reset. A login may
// verify bcrypt before locking, but cannot persist a stale password snapshot.
func lockSessionUser(tx *gorm.DB, token *models.RefreshToken) error {
	var user models.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", token.UserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if user.AuthVersion != token.AuthVersion || (token.PasswordHashSnapshot != "" && user.PasswordHash != token.PasswordHashSnapshot) {
		return ErrNotFound
	}
	return nil
}
