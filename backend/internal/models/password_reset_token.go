package models

import (
	"github.com/google/uuid"
	"time"
)

// PasswordResetToken never contains the bearer secret delivered by email.
type PasswordResetToken struct {
	BaseModel
	UserID     uuid.UUID  `gorm:"type:uuid;not null;index"`
	User       User       `gorm:"constraint:OnDelete:CASCADE" json:"-"`
	TokenHash  string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	ExpiresAt  time.Time  `gorm:"not null;index"`
	ConsumedAt *time.Time `gorm:"index"`
}
