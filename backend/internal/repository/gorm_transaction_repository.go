package repository

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"gorm.io/gorm"
)

type transactionRepository struct {
	db *gorm.DB
}

func NewTransactionRepository(db *gorm.DB) TransactionRepository {
	return &transactionRepository{
		db: db,
	}
}

func (r *transactionRepository) Create(transaction *models.Transaction) error {
	if !money.Valid(transaction.Amount) {
		return money.ErrRange
	}
	return financialMutation(r.db, transaction.UserID, func(tx *gorm.DB) error { return tx.Create(transaction).Error })
}

func (r *transactionRepository) FindByID(id uuid.UUID) (*models.Transaction, error) {
	var transaction models.Transaction
	err := r.db.First(&transaction, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &transaction, nil
}

func (r *transactionRepository) FindByUserID(userID uuid.UUID) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("user_id = ?", userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *transactionRepository) Update(userID uuid.UUID, transaction *models.Transaction) error {
	if !money.Valid(transaction.Amount) {
		return money.ErrRange
	}
	return financialMutation(r.db, userID, func(tx *gorm.DB) error {
		result := tx.Model(&models.Transaction{}).
			Where("id = ? AND user_id = ?", transaction.ID, userID).
			Updates(map[string]any{
				"name":        transaction.Name,
				"amount":      transaction.Amount,
				"account_id":  transaction.AccountID,
				"category_id": transaction.CategoryID,
				"date":        transaction.Date,
				"frequency":   transaction.Frequency,
				"type":        transaction.Type,
			})
		return mutationError(result)

	})
}

func (r *transactionRepository) Delete(userID, id uuid.UUID) error {
	return financialMutation(r.db, userID, func(tx *gorm.DB) error {
		return mutationError(tx.Delete(&models.Transaction{}, "id = ? AND user_id = ?", id, userID))

	})
}

func (r *transactionRepository) FindByAccountID(userID, accountID uuid.UUID) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("account_id = ? AND user_id = ?", accountID, userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *transactionRepository) FindByCategoryID(userID, categoryID uuid.UUID) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("category_id = ? AND user_id = ?", categoryID, userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *transactionRepository) FindByDateRange(userID uuid.UUID, startDate, endDate time.Time) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("date >= ? AND date < ? AND user_id = ?", startDate, endDate.AddDate(0, 0, 1), userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *transactionRepository) FindByFrequency(userID uuid.UUID, frequency models.Frequency) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("frequency = ? AND user_id = ?", frequency, userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}
