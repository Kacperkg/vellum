package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/kacperkg/vellum/internal/config"
	"github.com/kacperkg/vellum/internal/database"
	"github.com/kacperkg/vellum/internal/email"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/routes"
	"github.com/kacperkg/vellum/internal/services"
)

func main() {
	cfg := config.Load()

	// Startup schema work has a separate bounded pool so runtime query limits
	// cannot accidentally turn a larger existing-table migration into a 5s job.
	migrationCtx, cancelMigration := context.WithTimeout(context.Background(), 3*time.Minute)
	err := database.MigrateConfigured(migrationCtx, cfg)
	cancelMigration()
	if err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}
	db, err := database.Connect(cfg)
	if err != nil {
		log.Fatal("failed to connect to database")
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

	var sender services.PasswordResetSender
	if cfg.PasswordReset.Mode == "ses" {
		ctx, cancel := context.WithTimeout(context.Background(), cfg.PasswordReset.SendTimeout)
		sesSender, err := email.NewAWSSESSender(ctx, cfg.PasswordReset.Region, cfg.PasswordReset.FromAddress, cfg.PasswordReset.SendTimeout)
		cancel()
		if err != nil {
			log.Fatal("failed to initialize password recovery sender")
		}
		sender = sesSender
	}
	resetSvc, err := services.NewPasswordResetService(repository.NewPasswordResetRepository(db), sender, cfg.PasswordReset)
	if err != nil {
		log.Fatalf("invalid password recovery configuration: %v", err)
	}
	defer resetSvc.Close()
	// Handlers
	handlerSet := routes.HandlerSet{
		PasswordReset: handlers.NewPasswordResetHandler(resetSvc),
		Auth:          handlers.NewAuthHandler(userSvc),
		Accounts:      handlers.NewAccountHandler(accountSvc),
		Categories:    handlers.NewCategoryHandler(categorySvc),
		Budgets:       handlers.NewBudgetHandler(budgetSvc),
		Goals:         handlers.NewGoalHandler(goalSvc),
		Transactions:  handlers.NewTransactionHandler(transactionSvc),
		Transfers:     handlers.NewTransferHandler(transferSvc),
		Settings:      handlers.NewUserSettingsHandler(userSettingsSvc),
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
