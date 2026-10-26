package repository

import (
	"context"
	"errors"
	"time"

	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PasswordResetRepository interface {
	Issue(context.Context, string, string, time.Duration, time.Duration) (*models.PasswordResetToken, *models.User, error)
	Consume(context.Context, string, string) error
	Cleanup(context.Context, time.Time) error
}

type passwordResetRepository struct{ db *gorm.DB }

func NewPasswordResetRepository(db *gorm.DB) PasswordResetRepository {
	return &passwordResetRepository{db: db}
}

func (r *passwordResetRepository) Issue(ctx context.Context, email, hash string, ttl, cooldown time.Duration) (*models.PasswordResetToken, *models.User, error) {
	var user models.User
	var token models.PasswordResetToken
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Registration currently preserves exact email case. Do not ambiguously
		// merge existing case-distinct users during recovery.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("email = ?", email).First(&user).Error; err != nil {
			return err
		}
		now := time.Now()
		var recent int64
		if err := tx.Model(&models.PasswordResetToken{}).Where("user_id = ? AND created_at > ?", user.ID, now.Add(-cooldown)).Count(&recent).Error; err != nil {
			return err
		}
		if recent > 0 {
			return ErrNotFound
		}
		token = models.PasswordResetToken{UserID: user.ID, TokenHash: hash, ExpiresAt: now.Add(ttl)}
		// Outstanding links survive delivery failures; successful redemption consumes
		// every outstanding link. The user lock serializes issuance with redemption.
		return tx.Omit(clause.Associations).Create(&token).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	return &token, &user, nil
}

func (r *passwordResetRepository) Consume(ctx context.Context, hash, passwordHash string) error {
	// This lookup only discovers lock order. All validity checks occur again
	// after acquiring the user lock, including expiry evaluated after waiting.
	var candidate models.PasswordResetToken
	if err := r.db.WithContext(ctx).Where("token_hash = ?", hash).First(&candidate).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", candidate.UserID).Error; err != nil {
			return err
		}
		var token models.PasswordResetToken
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&token, "id = ?", candidate.ID).Error; err != nil {
			return err
		}
		now := time.Now()
		if token.ConsumedAt != nil || !token.ExpiresAt.After(now) {
			return ErrNotFound
		}
		if err := tx.Model(&user).Updates(map[string]any{"password_hash": passwordHash, "auth_version": gorm.Expr("auth_version + 1")}).Error; err != nil {
			return err
		}
		if err := tx.Model(&models.RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", now).Error; err != nil {
			return err
		}
		return tx.Model(&models.PasswordResetToken{}).Where("user_id = ? AND consumed_at IS NULL", user.ID).Update("consumed_at", now).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}

// Cleanup is bounded and deletes only records expired longer than retention.
// Retention must exceed the issuance cooldown to preserve its distributed cap.
func (r *passwordResetRepository) Cleanup(ctx context.Context, before time.Time) error {
	return r.db.WithContext(ctx).Exec(`DELETE FROM password_reset_tokens WHERE id IN (SELECT id FROM password_reset_tokens WHERE expires_at < ? ORDER BY expires_at LIMIT 1000)`, before).Error
}
