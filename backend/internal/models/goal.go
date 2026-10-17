package models

import "github.com/google/uuid"

type Goal struct {
	BaseModel

	UserID    uuid.UUID  `gorm:"type:uuid;not null"`
	User      User       `gorm:"foreignKey:UserID"`
	AccountID *uuid.UUID `gorm:"type:uuid;index"`
	Account   *Account   `gorm:"foreignKey:AccountID;constraint:OnDelete:RESTRICT"`

	Name                string `gorm:"not null"`
	TargetAmount        int64  `gorm:"not null"`
	SavedAmount         int64  `gorm:"not null"`
	MonthlyContribution int64  `gorm:"not null"`
}
