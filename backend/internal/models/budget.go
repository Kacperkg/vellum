package models

import "github.com/google/uuid"

const BudgetUserCategoryUniqueIndex = "idx_budgets_user_category"

type Budget struct {
	BaseModel

	UserID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_budgets_user_category,priority:1"`
	User   User      `gorm:"foreignKey:UserID"`

	CategoryID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_budgets_user_category,priority:2"`

	AllocatedAmount int64 `gorm:"not null"`
}
