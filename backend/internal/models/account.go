package models

import "github.com/google/uuid"

type Account struct {
	BaseModel

	UserID uuid.UUID `gorm:"type:uuid;not null"`
	User   User      `gorm:"foreignKey:UserID"`

	Name string `gorm:"not null"`

	// Read-only values computed by account queries, never persisted as balances.
	Balance         int64 `gorm:"->;-:migration"`
	AllocatedAmount int64 `gorm:"->;-:migration"`
	GoalCount       int64 `gorm:"->;-:migration"`
}
