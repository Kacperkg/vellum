package services_test

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
)

type fakeAccountRepository struct {
	Account  *models.Account
	Accounts []*models.Account
	Err      error
	Created  *models.Account
	Updated  *models.Account
	Deleted  uuid.UUID
}

func (r *fakeAccountRepository) Create(value *models.Account) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *fakeAccountRepository) GetByID(uuid.UUID) (*models.Account, error) { return r.Account, r.Err }
func (r *fakeAccountRepository) FindByUserID(uuid.UUID) ([]*models.Account, error) {
	return r.Accounts, r.Err
}
func (r *fakeAccountRepository) Update(_ uuid.UUID, value *models.Account) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *fakeAccountRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}

type fakeBudgetRepository struct {
	Budget  *models.Budget
	Budgets []*models.Budget
	Err     error
	Created *models.Budget
	Updated *models.Budget
	Deleted uuid.UUID
}

func (r *fakeBudgetRepository) FindByID(uuid.UUID) (*models.Budget, error) { return r.Budget, r.Err }
func (r *fakeBudgetRepository) FindByUserID(uuid.UUID) ([]*models.Budget, error) {
	return r.Budgets, r.Err
}
func (r *fakeBudgetRepository) Create(value *models.Budget) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *fakeBudgetRepository) Update(_ uuid.UUID, value *models.Budget) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *fakeBudgetRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}

type fakeCategoryRepository struct {
	Category   *models.Category
	Categories []*models.Category
	Err        error
}

func (r *fakeCategoryRepository) FindByID(uuid.UUID) (*models.Category, error) {
	return r.Category, r.Err
}
func (r *fakeCategoryRepository) ListAll() ([]*models.Category, error) { return r.Categories, r.Err }

type fakeGoalRepository struct {
	Goal    *models.Goal
	Goals   []*models.Goal
	Err     error
	Created *models.Goal
	Updated *models.Goal
	Deleted uuid.UUID
}

func (r *fakeGoalRepository) Create(value *models.Goal) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *fakeGoalRepository) FindByID(uuid.UUID) (*models.Goal, error)       { return r.Goal, r.Err }
func (r *fakeGoalRepository) FindByUserID(uuid.UUID) ([]*models.Goal, error) { return r.Goals, r.Err }
func (r *fakeGoalRepository) Update(_ uuid.UUID, value *models.Goal) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *fakeGoalRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}

type fakeTransactionRepository struct {
	Transaction  *models.Transaction
	Transactions []*models.Transaction
	Err          error
	Created      *models.Transaction
	Updated      *models.Transaction
	Deleted      uuid.UUID
}

func (r *fakeTransactionRepository) Create(value *models.Transaction) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *fakeTransactionRepository) FindByID(uuid.UUID) (*models.Transaction, error) {
	return r.Transaction, r.Err
}
func (r *fakeTransactionRepository) FindByUserID(uuid.UUID) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *fakeTransactionRepository) Update(_ uuid.UUID, value *models.Transaction) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *fakeTransactionRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}
func (r *fakeTransactionRepository) FindByCategoryID(uuid.UUID, uuid.UUID) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *fakeTransactionRepository) FindByDateRange(uuid.UUID, time.Time, time.Time) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *fakeTransactionRepository) FindByFrequency(uuid.UUID, models.Frequency) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *fakeTransactionRepository) FindByAccountID(uuid.UUID, uuid.UUID) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}

type fakeUserRepository struct {
	User                *models.User
	Err                 error
	RegistrationErr     error
	Created             *models.User
	CreatedSettings     *models.UserSettings
	CreatedRefreshToken *models.RefreshToken
}

func (r *fakeUserRepository) Create(value *models.User) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *fakeUserRepository) CreateRegistration(user *models.User, settings *models.UserSettings, token *models.RefreshToken) error {
	if r.RegistrationErr != nil {
		return r.RegistrationErr
	}
	if r.Err != nil {
		return r.Err
	}
	r.Created = user
	r.CreatedSettings = settings
	r.CreatedRefreshToken = token
	return nil
}
func (r *fakeUserRepository) FindByID(uuid.UUID) (*models.User, error) { return r.User, r.Err }
func (r *fakeUserRepository) FindByEmail(string) (*models.User, error) { return r.User, r.Err }

type fakeUserSettingsRepository struct {
	Settings *models.UserSettings
	Err      error
	Created  *models.UserSettings
	Updated  *models.UserSettings
}

func (r *fakeUserSettingsRepository) FindByUserID(uuid.UUID) (*models.UserSettings, error) {
	return r.Settings, r.Err
}
func (r *fakeUserSettingsRepository) Update(_ uuid.UUID, value *models.UserSettings) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *fakeUserSettingsRepository) Create(value *models.UserSettings) error {
	if r.Err == nil {
		r.Created = value
	}
	return r.Err
}

type fakeRefreshTokenRepository struct {
	Token   *models.RefreshToken
	Created *models.RefreshToken
	Err     error
}

func (r *fakeRefreshTokenRepository) Create(token *models.RefreshToken) error {
	if r.Err != nil {
		return r.Err
	}
	if token.ID == uuid.Nil {
		token.ID = uuid.New()
	}
	r.Token = token
	r.Created = token
	return nil
}

func (r *fakeRefreshTokenRepository) FindValid(hash string, now time.Time) (*models.RefreshToken, error) {
	if r.Err != nil {
		return nil, r.Err
	}
	if r.Token == nil || r.Token.TokenHash != hash || r.Token.RevokedAt != nil || !r.Token.ExpiresAt.After(now) {
		return nil, repository.ErrNotFound
	}
	return r.Token, nil
}

func (r *fakeRefreshTokenRepository) Rotate(currentID uuid.UUID, replacement *models.RefreshToken, now time.Time) error {
	if r.Err != nil {
		return r.Err
	}
	if r.Token == nil || r.Token.ID != currentID || r.Token.RevokedAt != nil {
		return repository.ErrNotFound
	}
	r.Token.RevokedAt = &now
	if replacement.ID == uuid.Nil {
		replacement.ID = uuid.New()
	}
	r.Token = replacement
	r.Created = replacement
	return nil
}

func (r *fakeRefreshTokenRepository) RevokeByHash(hash string, now time.Time) error {
	if r.Err != nil {
		return r.Err
	}
	if r.Token != nil && r.Token.TokenHash == hash && r.Token.RevokedAt == nil {
		r.Token.RevokedAt = &now
	}
	return nil
}

func (r *fakeUserRepository) FindByIDContext(context.Context, uuid.UUID) (*models.User, error) {
	return r.User, r.Err
}

func (r *fakeAccountRepository) WithContext(context.Context) repository.AccountRepository { return r }

func (r *fakeBudgetRepository) WithContext(context.Context) repository.BudgetRepository { return r }

func (r *fakeCategoryRepository) WithContext(context.Context) repository.CategoryRepository { return r }

func (r *fakeGoalRepository) WithContext(context.Context) repository.GoalRepository { return r }

func (r *fakeTransactionRepository) WithContext(context.Context) repository.TransactionRepository {
	return r
}

func (r *fakeUserSettingsRepository) WithContext(context.Context) repository.UserSettingsRepository {
	return r
}
