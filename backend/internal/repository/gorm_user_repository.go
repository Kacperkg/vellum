package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{
		db: db,
	}
}

func (r *userRepository) Create(user *models.User) error {
	return r.db.Create(user).Error
}

// CreateRegistration commits the complete initial account together. In
// particular, a failed settings or refresh-token insert must not strand a user
// whose email then prevents a retry.
func (r *userRepository) CreateRegistration(user *models.User, settings *models.UserSettings, refreshToken *models.RefreshToken) error {
	err := r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(user).Error; err != nil {
			return err
		}
		settings.UserID = user.ID
		refreshToken.UserID = user.ID
		if err := tx.Omit(clause.Associations).Create(settings).Error; err != nil {
			return err
		}
		return tx.Create(refreshToken).Error
	})
	var constraintError *pgconn.PgError
	if errors.As(err, &constraintError) && constraintError.Code == "23505" && constraintError.ConstraintName == "idx_users_email" {
		return ErrConflict
	}
	return err
}

func (r *userRepository) FindByID(id uuid.UUID) (*models.User, error) {
	return r.FindByIDContext(context.Background(), id)
}
func (r *userRepository) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.User, error) {
	var user models.User
	err := r.db.WithContext(ctx).First(&user, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) FindByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.db.First(&user, "email = ?", email).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}
