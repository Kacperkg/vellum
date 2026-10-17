package models

import (
	"time"

	"github.com/google/uuid"
)

// Transfer records both sides of an internal move in one row. It never creates
// income or expense transactions, so overall net worth remains unchanged.
type Transfer struct {
	BaseModel
	UserID        uuid.UUID `gorm:"type:uuid;not null;index"`
	User          User      `gorm:"foreignKey:UserID"`
	Name          string    `gorm:"not null"`
	Amount        int64     `gorm:"not null;check:transfer_amount_positive,amount > 0"`
	FromAccountID uuid.UUID `gorm:"type:uuid;not null;index;check:transfer_accounts_distinct,from_account_id <> to_account_id"`
	FromAccount   Account   `gorm:"foreignKey:FromAccountID;constraint:OnDelete:RESTRICT"`
	ToAccountID   uuid.UUID `gorm:"type:uuid;not null;index"`
	ToAccount     Account   `gorm:"foreignKey:ToAccountID;constraint:OnDelete:RESTRICT"`
	Date          time.Time `gorm:"not null"`
	Frequency     Frequency `gorm:"not null;default:one-off;check:transfer_frequency_valid,frequency IN ('one-off','daily','weekly','monthly','yearly')"`
}
