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
	HasActiveFamily(ctx context.Context, userID uuid.UUID, version int64, familyID uuid.UUID, now time.Time) (bool, error)
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
		var current models.RefreshToken
		if err := tx.First(&current, "id = ? AND user_id = ?", currentID, replacement.UserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		familyID := current.SessionFamilyID()
		// The service signs for this family before persistence. Never permit a
		// caller to rotate a credential into another session's family.
		if replacement.FamilyID != nil && *replacement.FamilyID != familyID {
			return ErrNotFound
		}
		replacement.FamilyID = &familyID
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

// RevokeByHash accepts an ancestor even after rotation or expiry. Possession of
// that opaque credential authorises ending only its own family, not other logins.
func (r *refreshTokenRepository) RevokeByHash(tokenHash string, now time.Time) error {
	ctx, cancel := context.WithTimeout(r.db.Statement.Context, 5*time.Second)
	defer cancel()
	var token models.RefreshToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", tokenHash).First(&token).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Same lock and order as rotation, login and password reset. If rotation
		// commits first its successor is included; if logout wins rotation fails.
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", token.UserID).Error; err != nil {
			return err
		}
		now = time.Now()
		familyID := token.SessionFamilyID()
		return tx.Model(&models.RefreshToken{}).
			Where("user_id = ? AND (family_id = ? OR (family_id IS NULL AND id = ?)) AND revoked_at IS NULL", token.UserID, familyID, familyID).
			Update("revoked_at", now).Error
	})
}

func (r *refreshTokenRepository) HasActiveFamily(ctx context.Context, userID uuid.UUID, version int64, familyID uuid.UUID, now time.Time) (bool, error) {
	var active bool
	err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (
		SELECT 1 FROM refresh_tokens WHERE user_id = ? AND auth_version = ?
		AND (family_id = ? OR (family_id IS NULL AND id = ?))
		AND revoked_at IS NULL AND expires_at > ?
	)`, userID, version, familyID, familyID, now).Scan(&active).Error
	return active, err
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
