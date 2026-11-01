package repository

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"gorm.io/gorm"
)

type accountRepository struct {
	db *gorm.DB
}

func NewAccountRepository(db *gorm.DB) AccountRepository {
	return &accountRepository{
		db: db,
	}
}

func (r *accountRepository) Create(account *models.Account) error {
	return r.db.Create(account).Error
}

func (r *accountRepository) GetByID(id uuid.UUID) (*models.Account, error) {
	var row accountSummary
	err := r.summaryQuery().Take(&row, "accounts.id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return row.account()
}

func (r *accountRepository) FindByUserID(userID uuid.UUID) ([]*models.Account, error) {
	var rows []accountSummary
	err := r.summaryQuery().Where("accounts.user_id = ?", userID).Find(&rows).Error
	if err != nil {
		return nil, err
	}
	accounts := make([]*models.Account, 0, len(rows))
	for _, row := range rows {
		account, err := row.account()
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func (r *accountRepository) Update(userID uuid.UUID, account *models.Account) error {
	result := r.db.Model(&models.Account{}).
		Where("id = ? AND user_id = ?", account.ID, userID).
		Update("name", account.Name)
	return mutationError(result)
}

// Each aggregate is correlated independently so multiple transactions, transfers
// and goals cannot multiply one another through a join. All are owner-scoped.
type accountSummary struct {
	models.Account
	BalanceText   string
	AllocatedText string
}

func (row accountSummary) account() (*models.Account, error) {
	account := row.Account
	balance, err := money.Parse(row.BalanceText)
	if err != nil {
		return &account, err
	}
	allocated, err := money.Parse(row.AllocatedText)
	if err != nil {
		return &account, err
	}
	if _, err := money.Subtract(balance, allocated); err != nil {
		return &account, err
	}
	account.Balance, account.AllocatedAmount = balance, allocated
	return &account, nil
}
func (r *accountRepository) summaryQuery() *gorm.DB {
	return r.db.Table(`(SELECT accounts.*, ` + accountTotals + `,
 (SELECT COUNT(*) FROM goals WHERE account_id = accounts.id AND user_id = accounts.user_id) AS goal_count FROM accounts) AS accounts`).Select(`accounts.id, accounts.created_at, accounts.updated_at, accounts.user_id, accounts.name,
 balance::text AS balance_text, allocated_amount::text AS allocated_text, goal_count`)
}

func (r *accountRepository) Delete(userID, id uuid.UUID) error {
	result := r.db.Delete(&models.Account{}, "id = ? AND user_id = ?", id, userID)
	var constraintError *pgconn.PgError
	if errors.As(result.Error, &constraintError) && constraintError.Code == "23503" {
		return ErrConflict
	}
	return mutationError(result)
}
