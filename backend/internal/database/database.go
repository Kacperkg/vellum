package database

import (
	"fmt"

	"github.com/kacperkg/vellum/internal/config"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Connect(cfg *config.Config) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		cfg.DBHost,
		cfg.DBPort,
		cfg.DBUser,
		cfg.DBPassword,
		cfg.DBName,
		cfg.DBSSLMode,
	)

	db, err := gorm.Open(
		postgres.Open(dsn),
		// SQL errors can contain credentials, password hashes or recipient data.
		// Services and workers emit fixed operational messages instead.
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)

	if err != nil {
		return nil, err
	}

	return db, nil
}
