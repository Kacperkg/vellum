package routes_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/handlers"
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

	return routes.HandlerSet{
		Auth:         handlers.NewAuthHandler(services.NewUserService(users, settings, &fakeRefreshTokenRepository{}, "secret", time.Hour, 30*24*time.Hour)),
		Accounts:     handlers.NewAccountHandler(services.NewAccountService(accounts)),
		Categories:   handlers.NewCategoryHandler(services.NewCategoryService(categories)),
		Budgets:      handlers.NewBudgetHandler(services.NewBudgetService(budgets, categories)),
		Goals:        handlers.NewGoalHandler(services.NewGoalService(goals)),
		Transactions: handlers.NewTransactionHandler(services.NewTransactionService(transactions, accounts, categories)),
		Settings:     handlers.NewUserSettingsHandler(services.NewUserSettingsService(settings)),
	}
}

func TestRegisteredRoutesAndAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	router := gin.New()
	routes.Register(router, "secret", testHandlerSet(userID))

	if got := len(router.Routes()); got != 33 {
		t.Fatalf("registered route count = %d, want 33", got)
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
	token, err := auth.GenerateToken(userID, "secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if recorder := request("/api/accounts", "Bearer "+token); recorder.Code != http.StatusOK {
		t.Fatalf("authenticated status = %d body = %s", recorder.Code, recorder.Body.String())
	}
}
