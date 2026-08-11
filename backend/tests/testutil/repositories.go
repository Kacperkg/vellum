package testutil

import (
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
)

type AccountRepository struct {
	Account  *models.Account
	Accounts []*models.Account
	Err      error
	Created  *models.Account
	Updated  *models.Account
	Deleted  uuid.UUID
}

func (r *AccountRepository) Create(value *models.Account) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *AccountRepository) GetByID(uuid.UUID) (*models.Account, error) { return r.Account, r.Err }
func (r *AccountRepository) FindByUserID(uuid.UUID) ([]*models.Account, error) {
	return r.Accounts, r.Err
}
func (r *AccountRepository) Update(_ uuid.UUID, value *models.Account) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *AccountRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}

type BudgetRepository struct {
	Budget  *models.Budget
	Budgets []*models.Budget
	Err     error
	Created *models.Budget
	Updated *models.Budget
	Deleted uuid.UUID
}

func (r *BudgetRepository) FindByID(uuid.UUID) (*models.Budget, error)       { return r.Budget, r.Err }
func (r *BudgetRepository) FindByUserID(uuid.UUID) ([]*models.Budget, error) { return r.Budgets, r.Err }
func (r *BudgetRepository) Create(value *models.Budget) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *BudgetRepository) Update(_ uuid.UUID, value *models.Budget) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *BudgetRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}

type CategoryRepository struct {
	Category   *models.Category
	Categories []*models.Category
	Err        error
}

func (r *CategoryRepository) FindByID(uuid.UUID) (*models.Category, error) { return r.Category, r.Err }
func (r *CategoryRepository) ListAll() ([]*models.Category, error)         { return r.Categories, r.Err }

type GoalRepository struct {
	Goal    *models.Goal
	Goals   []*models.Goal
	Err     error
	Created *models.Goal
	Updated *models.Goal
	Deleted uuid.UUID
}

func (r *GoalRepository) Create(value *models.Goal) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *GoalRepository) FindByID(uuid.UUID) (*models.Goal, error)       { return r.Goal, r.Err }
func (r *GoalRepository) FindByUserID(uuid.UUID) ([]*models.Goal, error) { return r.Goals, r.Err }
func (r *GoalRepository) Update(_ uuid.UUID, value *models.Goal) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *GoalRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}

type TransactionRepository struct {
	Transaction  *models.Transaction
	Transactions []*models.Transaction
	Err          error
	Created      *models.Transaction
	Updated      *models.Transaction
	Deleted      uuid.UUID
}

func (r *TransactionRepository) Create(value *models.Transaction) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *TransactionRepository) FindByID(uuid.UUID) (*models.Transaction, error) {
	return r.Transaction, r.Err
}
func (r *TransactionRepository) FindByUserID(uuid.UUID) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *TransactionRepository) Update(_ uuid.UUID, value *models.Transaction) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *TransactionRepository) Delete(_ uuid.UUID, id uuid.UUID) error {
	if r.Err == nil {
		r.Deleted = id
	}
	return r.Err
}
func (r *TransactionRepository) FindByCategoryID(uuid.UUID, uuid.UUID) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *TransactionRepository) FindByDateRange(uuid.UUID, time.Time, time.Time) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *TransactionRepository) FindByFrequency(uuid.UUID, models.Frequency) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}
func (r *TransactionRepository) FindByAccountID(uuid.UUID, uuid.UUID) ([]*models.Transaction, error) {
	return r.Transactions, r.Err
}

type UserRepository struct {
	User    *models.User
	Err     error
	Created *models.User
}

func (r *UserRepository) Create(value *models.User) error {
	if r.Err == nil {
		value.ID = uuid.New()
		r.Created = value
	}
	return r.Err
}
func (r *UserRepository) FindByID(uuid.UUID) (*models.User, error) { return r.User, r.Err }
func (r *UserRepository) FindByEmail(string) (*models.User, error) { return r.User, r.Err }

type UserSettingsRepository struct {
	Settings *models.UserSettings
	Err      error
	Created  *models.UserSettings
	Updated  *models.UserSettings
}

func (r *UserSettingsRepository) FindByUserID(uuid.UUID) (*models.UserSettings, error) {
	return r.Settings, r.Err
}
func (r *UserSettingsRepository) Update(_ uuid.UUID, value *models.UserSettings) error {
	if r.Err == nil {
		r.Updated = value
	}
	return r.Err
}
func (r *UserSettingsRepository) Create(value *models.UserSettings) error {
	if r.Err == nil {
		r.Created = value
	}
	return r.Err
}
