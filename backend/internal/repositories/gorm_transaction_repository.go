package repositories

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type GormTransactionRepository struct {
	db *gorm.DB
}

func NewGormTransactionRepository(db *gorm.DB) *GormTransactionRepository {
	return &GormTransactionRepository{
		db: db,
	}
}

func (r *GormTransactionRepository) Create(transaction *models.Transaction) error {
	return r.db.Create(transaction).Error
}

func (r *GormTransactionRepository) FindByID(id uuid.UUID) (*models.Transaction, error) {
	var transaction models.Transaction
	err := r.db.First(&transaction, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &transaction, nil
}

func (r *GormTransactionRepository) FindByUserID(userID uuid.UUID) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("user_id = ?", userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *GormTransactionRepository) Update(userID uuid.UUID, transaction *models.Transaction) error {
	result := r.db.Model(&models.Transaction{}).
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
}

func (r *GormTransactionRepository) Delete(userID, id uuid.UUID) error {
	return mutationError(r.db.Delete(&models.Transaction{}, "id = ? AND user_id = ?", id, userID))
}

func (r *GormTransactionRepository) FindByAccountID(userID, accountID uuid.UUID) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("account_id = ? AND user_id = ?", accountID, userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *GormTransactionRepository) FindByCategoryID(userID, categoryID uuid.UUID) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("category_id = ? AND user_id = ?", categoryID, userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *GormTransactionRepository) FindByDateRange(userID uuid.UUID, startDate, endDate time.Time) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("date >= ? AND date <= ? AND user_id = ?", startDate, endDate, userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}

func (r *GormTransactionRepository) FindByFrequency(userID uuid.UUID, frequency models.Frequency) ([]*models.Transaction, error) {
	var transactions []*models.Transaction
	err := r.db.Where("frequency = ? AND user_id = ?", frequency, userID).Find(&transactions).Error
	if err != nil {
		return nil, err
	}
	return transactions, nil
}
