package models

import (
	"github.com/google/uuid"
)

type UserSettings struct {
	BaseModel

	UserID uuid.UUID `gorm:"type:uuid;not null"`
	User   User      `gorm:"foreignKey:UserID"`

	Currency     Currency     `gorm:"not null"`
	DateFormat   DateFormat   `gorm:"not null"`
	NumberFormat NumberFormat `gorm:"not null"`
	WeekStartsOn WeekStartsOn `gorm:"not null"`
}
