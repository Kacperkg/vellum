package main

import (
	"log"
	"net"
	"net/http"
	"time"

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
	transferRepo := repository.NewTransferRepository(db)

	// Services
	userSvc := services.NewUserService(userRepo, refreshTokenRepo, cfg.JWTSecret, jwtExpiry, refreshExpiry)
	userSettingsSvc := services.NewUserSettingsService(userSettingsRepo)
	accountSvc := services.NewAccountService(accountRepo)
	categorySvc := services.NewCategoryService(categoryRepo)
	budgetSvc := services.NewBudgetService(budgetRepo, categoryRepo)
	goalSvc := services.NewGoalService(goalRepo, accountRepo)
	transactionSvc := services.NewTransactionService(transactionRepo, accountRepo, categoryRepo)
	transferSvc := services.NewTransferService(transferRepo, accountRepo)

	// Handlers
	handlerSet := routes.HandlerSet{
		Auth:         handlers.NewAuthHandler(userSvc),
		Accounts:     handlers.NewAccountHandler(accountSvc),
		Categories:   handlers.NewCategoryHandler(categorySvc),
		Budgets:      handlers.NewBudgetHandler(budgetSvc),
		Goals:        handlers.NewGoalHandler(goalSvc),
		Transactions: handlers.NewTransactionHandler(transactionSvc),
		Transfers:    handlers.NewTransferHandler(transferSvc),
		Settings:     handlers.NewUserSettingsHandler(userSettingsSvc),
	}

	router, err := routes.NewRouter(cfg, handlerSet)
	if err != nil {
		log.Fatalf("invalid router configuration: %v", err)
	}
	server := newHTTPServer(net.JoinHostPort(cfg.BindHost, cfg.Port), router)

	log.Printf("Vellum API listening on :%s", cfg.Port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func newHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}
