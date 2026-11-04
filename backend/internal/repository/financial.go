package repository

import (
	"errors"
	"math/big"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Cast before negation so even a legacy MinInt64 cannot overflow PostgreSQL
// bigint arithmetic. SUM and every subsequent operation remain numeric.
const accountTotals = `
 COALESCE((SELECT SUM(CASE WHEN type = 'income' THEN amount::numeric WHEN type = 'expense' THEN -amount::numeric ELSE 0 END)
 FROM transactions WHERE account_id = accounts.id AND user_id = accounts.user_id), 0)
 + COALESCE((SELECT SUM(amount::numeric) FROM transfers WHERE to_account_id = accounts.id AND user_id = accounts.user_id), 0)
 - COALESCE((SELECT SUM(amount::numeric) FROM transfers WHERE from_account_id = accounts.id AND user_id = accounts.user_id), 0) AS balance,
 COALESCE((SELECT SUM(target_amount::numeric) FROM goals WHERE account_id = accounts.id AND user_id = accounts.user_id), 0) AS allocated_amount`

// Account API outputs are bounded numbers. Frontend-only sums use BigInt and
// therefore do not impose an arbitrary lifetime/gross-turnover cap on a user.
func financialExcess(db *gorm.DB, userID uuid.UUID) (map[string]*big.Int, error) {
	var rows []struct {
		ID        uuid.UUID
		Balance   string
		Allocated string
		Available string
	}
	err := db.Raw(`SELECT id, balance::text, allocated_amount::text AS allocated,
 (balance - allocated_amount)::text AS available
 FROM (SELECT accounts.id, `+accountTotals+` FROM accounts WHERE accounts.user_id = ?) totals`, userID).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]*big.Int, len(rows)*3)
	for _, row := range rows {
		for name, value := range map[string]string{"balance": row.Balance, "allocated": row.Allocated, "available": row.Available} {
			excess, err := money.Excess(value)
			if err != nil {
				return nil, err
			}
			result[row.ID.String()+name] = excess
		}
	}
	return result, nil
}

// Serialise financial mutations affecting account summaries for one owner,
// including deletes: removing
// an offsetting income/transfer can otherwise reveal an unsupported balance.
// Validate in the same transaction, rolling back all writes on rejection. A
// legacy violation may stay unchanged or shrink, allowing incremental repair;
// no operation may introduce a new violation or enlarge an existing one.
func financialMutation(db *gorm.DB, userID uuid.UUID, mutate func(*gorm.DB) error) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var user models.User
		if err := tx.Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", userID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		before, err := financialExcess(tx, userID)
		if err != nil {
			return err
		}
		if err := mutate(tx); err != nil {
			return err
		}
		after, err := financialExcess(tx, userID)
		if err != nil {
			return err
		}
		for key, excess := range after {
			previous := before[key]
			if previous == nil {
				previous = new(big.Int)
			}
			if excess.Cmp(previous) > 0 {
				return money.ErrRange
			}
		}
		return nil
	})
}
