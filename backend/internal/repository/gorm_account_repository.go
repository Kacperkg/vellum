package repository

import (
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kacperkg/vellum/internal/models"
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
	var account models.Account
	err := r.summaryQuery().First(&account, "accounts.id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &account, nil
}

func (r *accountRepository) FindByUserID(userID uuid.UUID) ([]*models.Account, error) {
	var accounts []*models.Account
	err := r.summaryQuery().Where("accounts.user_id = ?", userID).Find(&accounts).Error
	if err != nil {
		return nil, err
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
func (r *accountRepository) summaryQuery() *gorm.DB {
	return r.db.Model(&models.Account{}).Select(`accounts.*,
		COALESCE((SELECT SUM(CASE WHEN type = 'income' THEN amount WHEN type = 'expense' THEN -amount ELSE 0 END)
			FROM transactions WHERE account_id = accounts.id AND user_id = accounts.user_id), 0)
		+ COALESCE((SELECT SUM(amount) FROM transfers WHERE to_account_id = accounts.id AND user_id = accounts.user_id), 0)
		- COALESCE((SELECT SUM(amount) FROM transfers WHERE from_account_id = accounts.id AND user_id = accounts.user_id), 0) AS balance,
		COALESCE((SELECT SUM(target_amount) FROM goals WHERE account_id = accounts.id AND user_id = accounts.user_id), 0) AS allocated_amount,
		(SELECT COUNT(*) FROM goals WHERE account_id = accounts.id AND user_id = accounts.user_id) AS goal_count`)
}

func (r *accountRepository) Delete(userID, id uuid.UUID) error {
	result := r.db.Delete(&models.Account{}, "id = ? AND user_id = ?", id, userID)
	var constraintError *pgconn.PgError
	if errors.As(result.Error, &constraintError) && constraintError.Code == "23503" {
		return ErrConflict
	}
	return mutationError(result)
}
