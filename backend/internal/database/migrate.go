package database

import (
	"fmt"

	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var canonicalCategoryNames = []string{
	"Groceries",
	"Dining",
	"Transport",
	"Housing",
	"Utilities",
	"Entertainment",
	"Shopping",
	"Health",
	"Travel",
	"Subscriptions",
	"Income",
}

func Migrate(db *gorm.DB) error {
	// Check legacy records before AutoMigrate creates the unique budget index.
	// Never guess which financial allocation to keep or silently merge amounts.
	if err := preflightBudgetUniqueness(db); err != nil {
		return err
	}

	if err := db.AutoMigrate(
		&models.User{},
		&models.RefreshToken{},
		&models.UserSettings{},
		&models.Account{},
		&models.Category{},
		&models.Transaction{},
		&models.Transfer{},
		&models.Budget{},
		&models.Goal{},
	); err != nil {
		return err
	}

	return seedCanonicalCategories(db)
}

func preflightBudgetUniqueness(db *gorm.DB) error {
	if !db.Migrator().HasTable(&models.Budget{}) {
		return nil
	}
	var duplicateGroups int64
	if err := db.Raw(`SELECT COUNT(*) FROM (
		SELECT 1 FROM budgets GROUP BY user_id, category_id HAVING COUNT(*) > 1
	) AS duplicate_budgets`).Scan(&duplicateGroups).Error; err != nil {
		return fmt.Errorf("check budget uniqueness before migration: %w", err)
	}
	if duplicateGroups > 0 {
		return fmt.Errorf("cannot migrate budgets: %d duplicate user/category groups exist; review and resolve those allocations without losing financial data, then retry migration; no records were changed", duplicateGroups)
	}
	return nil
}

func seedCanonicalCategories(db *gorm.DB) error {
	categories := make([]models.Category, 0, len(canonicalCategoryNames))
	for _, name := range canonicalCategoryNames {
		categories = append(categories, models.Category{Name: name})
	}

	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoNothing: true,
	}).Create(&categories).Error
}
