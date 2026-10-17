package repository

import (
	"errors"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

type transferRepository struct{ db *gorm.DB }

func NewTransferRepository(db *gorm.DB) TransferRepository {
	return &transferRepository{db: db}
}

func (r *transferRepository) Create(transfer *models.Transfer) error {
	return r.db.Create(transfer).Error
}

func (r *transferRepository) FindByID(id uuid.UUID) (*models.Transfer, error) {
	var transfer models.Transfer
	if err := r.db.First(&transfer, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &transfer, nil
}

func (r *transferRepository) FindByUserID(userID uuid.UUID) ([]*models.Transfer, error) {
	transfers := make([]*models.Transfer, 0)
	err := r.db.Where("user_id = ?", userID).Order("date DESC, created_at DESC").Find(&transfers).Error
	return transfers, err
}

func (r *transferRepository) Update(userID uuid.UUID, transfer *models.Transfer) error {
	result := r.db.Model(&models.Transfer{}).Where("id = ? AND user_id = ?", transfer.ID, userID).Updates(map[string]any{
		"name":            transfer.Name,
		"amount":          transfer.Amount,
		"from_account_id": transfer.FromAccountID,
		"to_account_id":   transfer.ToAccountID,
		"date":            transfer.Date,
		"frequency":       transfer.Frequency,
	})
	return mutationError(result)
}

func (r *transferRepository) Delete(userID, id uuid.UUID) error {
	return mutationError(r.db.Delete(&models.Transfer{}, "id = ? AND user_id = ?", id, userID))
}
