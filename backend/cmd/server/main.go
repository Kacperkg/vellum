package main

import (
	"log"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/config"
	"github.com/kacperkg/vellum/internal/database"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/routes"
	"github.com/kacperkg/vellum/internal/services"
)

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	jwtExpiry, err := time.ParseDuration(cfg.JWTExpiry)
	if err != nil {
		log.Fatalf("invalid JWT expiry: %v", err)
	}
	refreshExpiry, err := time.ParseDuration(cfg.JWTRefreshExpiry)
	if err != nil {
		log.Fatalf("invalid JWT refresh expiry: %v", err)
	}

	// Repositories
	userRepo := repository.NewUserRepository(db)
	userSettingsRepo := repository.NewUserSettingsRepository(db)
	refreshTokenRepo := repository.NewRefreshTokenRepository(db)
	accountRepo := repository.NewAccountRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
	budgetRepo := repository.NewBudgetRepository(db)
	goalRepo := repository.NewGoalRepository(db)
	transactionRepo := repository.NewTransactionRepository(db)

	// Services
	userSvc := services.NewUserService(userRepo, userSettingsRepo, refreshTokenRepo, cfg.JWTSecret, jwtExpiry, refreshExpiry)
	userSettingsSvc := services.NewUserSettingsService(userSettingsRepo)
	accountSvc := services.NewAccountService(accountRepo)
	categorySvc := services.NewCategoryService(categoryRepo)
	budgetSvc := services.NewBudgetService(budgetRepo, categoryRepo)
	goalSvc := services.NewGoalService(goalRepo)
	transactionSvc := services.NewTransactionService(transactionRepo, accountRepo, categoryRepo)

	// Handlers
	handlerSet := routes.HandlerSet{
		Auth:         handlers.NewAuthHandler(userSvc),
		Accounts:     handlers.NewAccountHandler(accountSvc),
		Categories:   handlers.NewCategoryHandler(categorySvc),
		Budgets:      handlers.NewBudgetHandler(budgetSvc),
		Goals:        handlers.NewGoalHandler(goalSvc),
		Transactions: handlers.NewTransactionHandler(transactionSvc),
		Settings:     handlers.NewUserSettingsHandler(userSettingsSvc),
	}

	router := gin.Default()
	router.Use(cors.New(cors.Config{
		AllowOrigins: []string{"http://localhost:5173"},
		AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders: []string{"Origin", "Content-Type", "Authorization"},
	}))
	routes.Register(router, cfg.JWTSecret, handlerSet)

	log.Printf("Vellum API listening on :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
