package routes_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/middleware"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/routes"
	"github.com/kacperkg/vellum/internal/services"
)

func testHandlerSet(userID uuid.UUID) routes.HandlerSet {
	users := &fakeUserRepository{}
	settings := &fakeUserSettingsRepository{Settings: &models.UserSettings{UserID: userID}}
	accounts := &fakeAccountRepository{Accounts: []*models.Account{}}
	categories := &fakeCategoryRepository{Categories: []*models.Category{}}
	budgets := &fakeBudgetRepository{Budgets: []*models.Budget{}}
	goals := &fakeGoalRepository{Goals: []*models.Goal{}}
	transactions := &fakeTransactionRepository{Transactions: []*models.Transaction{}}
	transfers := &fakeTransferRepository{}

	return routes.HandlerSet{
		Auth:         handlers.NewAuthHandler(services.NewUserService(users, &fakeRefreshTokenRepository{}, "secret", time.Hour, 30*24*time.Hour)),
		Accounts:     handlers.NewAccountHandler(services.NewAccountService(accounts)),
		Categories:   handlers.NewCategoryHandler(services.NewCategoryService(categories)),
		Budgets:      handlers.NewBudgetHandler(services.NewBudgetService(budgets, categories)),
		Goals:        handlers.NewGoalHandler(services.NewGoalService(goals, accounts)),
		Transactions: handlers.NewTransactionHandler(services.NewTransactionService(transactions, accounts, categories)),
		Transfers:    handlers.NewTransferHandler(services.NewTransferService(transfers, accounts)),
		Settings:     handlers.NewUserSettingsHandler(services.NewUserSettingsService(settings)),
	}
}

func TestAuthenticationRoutesApplyRateLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	limiter := middleware.NewAuthRateLimiter(map[middleware.AuthEndpoint]middleware.RateLimit{
		middleware.LoginEndpoint:    {Requests: 1, Window: time.Minute},
		middleware.RegisterEndpoint: {Requests: 1, Window: time.Minute},
		middleware.RefreshEndpoint:  {Requests: 1, Window: time.Minute},
		middleware.LogoutEndpoint:   {Requests: 1, Window: time.Minute},
	}, 128, 20)
	routes.Register(router, "secret", testHandlerSet(uuid.New()), limiter)

	request := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"missing@example.test","password":"password"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = "192.0.2.10:1234"
		router.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := request(); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("first status = %d, want 401", recorder.Code)
	}
	if recorder := request(); recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("second status = %d, want 429", recorder.Code)
	}
}

func TestRegisteredRoutesAndAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	router := gin.New()
	limiter := middleware.NewAuthRateLimiter(map[middleware.AuthEndpoint]middleware.RateLimit{
		middleware.LoginEndpoint:    {Requests: 10, Window: time.Minute},
		middleware.RegisterEndpoint: {Requests: 5, Window: time.Hour},
		middleware.RefreshEndpoint:  {Requests: 30, Window: time.Minute},
		middleware.LogoutEndpoint:   {Requests: 30, Window: time.Minute},
	}, 10000, 20)
	routes.Register(router, "secret", testHandlerSet(userID), limiter)

	if got := len(router.Routes()); got != 38 {
		t.Fatalf("registered route count = %d, want 38", got)
	}

	request := func(path, authorization string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if authorization != "" {
			req.Header.Set("Authorization", authorization)
		}
		router.ServeHTTP(recorder, req)
		return recorder
	}

	if recorder := request("/ping", ""); recorder.Code != http.StatusOK {
		t.Fatalf("ping status = %d", recorder.Code)
	}
	if recorder := request("/api/accounts", ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", recorder.Code)
	}
	if recorder := request("/api/transfers", ""); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated transfers status = %d", recorder.Code)
	}
	token, err := auth.GenerateToken(userID, "secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if recorder := request("/api/accounts", "Bearer "+token); recorder.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d body = %s", recorder.Code, recorder.Body.String())
	}
	if recorder := request("/api/transfers", "Bearer "+token); recorder.Code != http.StatusOK {
		t.Fatalf("authenticated transfers status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
