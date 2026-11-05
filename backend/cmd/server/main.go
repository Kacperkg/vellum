package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
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
	mode := startupMode(os.Args[1:])
	if mode == "migration-contract" {
		log.SetFlags(0)
		contract, err := database.MigrationContract()
		if err != nil {
			log.Fatal(err)
		}
		os.Stdout.WriteString(contract + "\n")
		return
	}
	if mode == "migrate" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		err := database.MigrateControlled(ctx, config.LoadDatabase())
		cancel()
		if err != nil {
			log.Fatal(err)
		}
		return
	}
	serve(mode)
}

func startupMode(args []string) string {
	if len(args) == 0 {
		return "legacy"
	}
	if len(args) == 1 {
		switch args[0] {
		case "serve", "migrate", "migration-contract":
			return args[0]
		}
	}
	log.Fatal("usage: vellum [serve | migrate | migration-contract]")
	return "" // log.Fatal terminates the process.
}

func serve(mode string) {
	cfg := config.Load()

	var dbErr error
	if mode == "legacy" {
		// Existing deployments and local Air startup still use this entry point.
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		dbErr = database.MigrateConfigured(ctx, cfg)
		cancel()
		if dbErr != nil {
			log.Fatalf("failed to run migrations: %v", dbErr)
		}
	}
	db, err := database.ConnectForServing(cfg, mode == "serve")
	if err != nil {
		log.Fatal(err)
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
