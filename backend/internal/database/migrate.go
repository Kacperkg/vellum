package database

import (
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
