package repository

import (
	"errors"

	"github.com/google/uuid"
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
	err := r.db.First(&account, "id = ?", id).Error
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
	err := r.db.Where("user_id = ?", userID).Find(&accounts).Error
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

func (r *accountRepository) Delete(userID, id uuid.UUID) error {
	return mutationError(r.db.Delete(&models.Account{}, "id = ? AND user_id = ?", id, userID))
}
