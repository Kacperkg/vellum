package database

import (
	"github.com/kacperkg/vellum/internal/models"
	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&models.User{},
		&models.UserSettings{},
		&models.Account{},
		&models.Category{},
		&models.Transaction{},
		&models.Budget{},
		&models.Goal{},
	)
}
