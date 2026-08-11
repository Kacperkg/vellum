package models

import (
	"time"

	"github.com/google/uuid"
)

type Transaction struct {
	BaseModel

	UserID uuid.UUID `gorm:"type:uuid;not null"`
	User   User      `gorm:"foreignKey:UserID"`

	Name   string `gorm:"not null"`
	Amount int64  `gorm:"not null"`

	AccountID uuid.UUID `gorm:"type:uuid;not null"`
	Account   Account   `gorm:"foreignKey:AccountID"`

	CategoryID uuid.UUID `gorm:"type:uuid;not null"`
	Category   Category  `gorm:"foreignKey:CategoryID"`

	Date time.Time `gorm:"not null"`

	Frequency Frequency       `gorm:"not null"`
	Type      TransactionType `gorm:"not null"`
}
