package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
	"golang.org/x/crypto/bcrypt"
)

func init() { gin.SetMode(gin.TestMode) }

func authenticatedRouter(userID uuid.UUID) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("userID", userID); c.Next() })
	return router
}

func perform(router http.Handler, method, path, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func requireStatus(t *testing.T, recorder *httptest.ResponseRecorder, want int) {
	t.Helper()
	if recorder.Code != want {
		t.Fatalf("status=%d want=%d body=%s", recorder.Code, want, recorder.Body.String())
	}
}

func TestAccountHandler(t *testing.T) {
	userID, accountID := uuid.New(), uuid.New()
	repo := &fakeAccountRepository{Account: &models.Account{BaseModel: models.BaseModel{ID: accountID}, UserID: userID, Name: "Main"}, Accounts: []*models.Account{{BaseModel: models.BaseModel{ID: accountID}, UserID: userID, Name: "Main"}}}
	handler := handlers.NewAccountHandler(services.NewAccountService(repo))
	router := authenticatedRouter(userID)
	router.POST("/accounts", handler.Create)
	router.GET("/accounts", handler.ListByUser)
	router.GET("/accounts/:id", handler.GetByID)
	router.PUT("/accounts/:id", handler.Update)
	router.DELETE("/accounts/:id", handler.Delete)

	requireStatus(t, perform(router, http.MethodPost, "/accounts", `{"name":"Savings"}`), http.StatusCreated)
	requireStatus(t, perform(router, http.MethodGet, "/accounts", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/accounts/"+accountID.String(), ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodPut, "/accounts/"+accountID.String(), `{"name":"Updated"}`), http.StatusOK)
	requireStatus(t, perform(router, http.MethodDelete, "/accounts/"+accountID.String(), ""), http.StatusNoContent)
	requireStatus(t, perform(router, http.MethodPost, "/accounts", `{`), http.StatusBadRequest)
	requireStatus(t, perform(router, http.MethodGet, "/accounts/not-a-uuid", ""), http.StatusBadRequest)

	unauthenticated := gin.New()
	unauthenticated.GET("/accounts", handler.ListByUser)
	requireStatus(t, perform(unauthenticated, http.MethodGet, "/accounts", ""), http.StatusUnauthorized)
	repo.Account.UserID = uuid.New()
	requireStatus(t, perform(router, http.MethodGet, "/accounts/"+accountID.String(), ""), http.StatusNotFound)

	repo.Err = errors.New("database password leaked")
	recorder := perform(router, http.MethodGet, "/accounts", "")
	requireStatus(t, recorder, http.StatusInternalServerError)
	if strings.Contains(recorder.Body.String(), repo.Err.Error()) {
		t.Fatalf("internal error leaked: %s", recorder.Body.String())
	}
}

func TestBudgetHandler(t *testing.T) {
	userID, budgetID, categoryID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeBudgetRepository{Budget: &models.Budget{BaseModel: models.BaseModel{ID: budgetID}, UserID: userID, CategoryID: categoryID, AllocatedAmount: 10000}}
	categoryRepo := &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}}}
	handler := handlers.NewBudgetHandler(services.NewBudgetService(repo, categoryRepo))
	router := authenticatedRouter(userID)
	router.POST("/budgets", handler.Create)
	router.GET("/budgets", handler.ListByUser)
	router.GET("/budgets/:id", handler.FindByID)
	router.PUT("/budgets/:id", handler.Update)
	router.DELETE("/budgets/:id", handler.Delete)
	body := `{"category_id":"` + categoryID.String() + `","allocated_amount":15000}`
	requireStatus(t, perform(router, http.MethodPost, "/budgets", body), http.StatusCreated)
	requireStatus(t, perform(router, http.MethodGet, "/budgets", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/budgets/"+budgetID.String(), ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodPut, "/budgets/"+budgetID.String(), body), http.StatusOK)
	requireStatus(t, perform(router, http.MethodDelete, "/budgets/"+budgetID.String(), ""), http.StatusNoContent)
	requireStatus(t, perform(router, http.MethodPost, "/budgets", `{"allocated_amount":0}`), http.StatusBadRequest)
}

func TestCategoryHandler(t *testing.T) {
	categoryID := uuid.New()
	repo := &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}, Name: "Food"}, Categories: []*models.Category{{Name: "Food"}}}
	handler := handlers.NewCategoryHandler(services.NewCategoryService(repo))
	router := gin.New()
	router.GET("/categories", handler.ListAll)
	router.GET("/categories/:id", handler.FindByID)
	requireStatus(t, perform(router, http.MethodGet, "/categories", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/categories/"+categoryID.String(), ""), http.StatusOK)
	repo.Category = nil
	requireStatus(t, perform(router, http.MethodGet, "/categories/"+categoryID.String(), ""), http.StatusNotFound)
}

func TestGoalHandler(t *testing.T) {
	userID, goalID := uuid.New(), uuid.New()
	goal := &models.Goal{BaseModel: models.BaseModel{ID: goalID}, UserID: userID, Name: "Emergency", TargetAmount: 100000, SavedAmount: 10000}
	repo := &fakeGoalRepository{Goal: goal, Goals: []*models.Goal{goal}}
	handler := handlers.NewGoalHandler(services.NewGoalService(repo, &fakeAccountRepository{}))
	router := authenticatedRouter(userID)
	router.POST("/goals", handler.Create)
	router.GET("/goals", handler.ListByUser)
	router.GET("/goals/:id", handler.GetByID)
	router.PUT("/goals/:id", handler.Update)
	router.DELETE("/goals/:id", handler.Delete)
	body := `{"name":"Emergency","target_amount":100000,"saved_amount":10000,"monthly_contribution":5000}`
	requireStatus(t, perform(router, http.MethodPost, "/goals", body), http.StatusCreated)
	requireStatus(t, perform(router, http.MethodGet, "/goals", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/goals/"+goalID.String(), ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodPut, "/goals/"+goalID.String(), body), http.StatusOK)
	requireStatus(t, perform(router, http.MethodDelete, "/goals/"+goalID.String(), ""), http.StatusNoContent)
	requireStatus(t, perform(router, http.MethodPost, "/goals", `{"name":"x","target_amount":100,"saved_amount":101}`), http.StatusBadRequest)
}

func TestTransactionHandler(t *testing.T) {
	userID, transactionID, accountID, categoryID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tx := &models.Transaction{BaseModel: models.BaseModel{ID: transactionID}, UserID: userID, Name: "Salary", Amount: 100000, AccountID: accountID, CategoryID: categoryID, Date: time.Now(), Frequency: models.Monthly, Type: models.Income}
	repo := &fakeTransactionRepository{Transaction: tx, Transactions: []*models.Transaction{tx}}
	service := services.NewTransactionService(repo, &fakeAccountRepository{Account: &models.Account{BaseModel: models.BaseModel{ID: accountID}, UserID: userID}}, &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}}})
	handler := handlers.NewTransactionHandler(service)
	router := authenticatedRouter(userID)
	router.POST("/transactions", handler.Create)
	router.GET("/transactions", handler.ListByUser)
	router.GET("/transactions/:id", handler.GetByID)
	router.PUT("/transactions/:id", handler.Update)
	router.DELETE("/transactions/:id", handler.Delete)
	router.GET("/accounts/:account_id/transactions", handler.ListByAccount)
	router.GET("/categories/:category_id/transactions", handler.ListByCategory)
	router.GET("/transaction-range", handler.ListByDateRange)
	router.GET("/transaction-frequency", handler.ListByFrequency)
	body := `{"name":"Salary","amount":100000,"account_id":"` + accountID.String() + `","category_id":"` + categoryID.String() + `","date":"2026-08-11T12:00:00Z","frequency":"monthly","type":"income"}`
	requireStatus(t, perform(router, http.MethodPost, "/transactions", body), http.StatusCreated)
	requireStatus(t, perform(router, http.MethodGet, "/transactions", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/transactions/"+transactionID.String(), ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodPut, "/transactions/"+transactionID.String(), body), http.StatusOK)
	requireStatus(t, perform(router, http.MethodDelete, "/transactions/"+transactionID.String(), ""), http.StatusNoContent)
	requireStatus(t, perform(router, http.MethodGet, "/accounts/"+accountID.String()+"/transactions", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/categories/"+categoryID.String()+"/transactions", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/transaction-range?start_date=2026-08-01T00:00:00Z&end_date=2026-08-31T23:59:59Z", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/transaction-frequency?frequency=monthly", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/transaction-range?start_date=bad&end_date=bad", ""), http.StatusBadRequest)
	requireStatus(t, perform(router, http.MethodGet, "/transaction-frequency?frequency=bad", ""), http.StatusBadRequest)
}

func TestAuthHandler(t *testing.T) {
	const secret = "test-secret"
	t.Run("register", func(t *testing.T) {
		handler := handlers.NewAuthHandler(services.NewUserService(&fakeUserRepository{}, &fakeRefreshTokenRepository{}, secret, time.Hour, 30*24*time.Hour))
		router := gin.New()
		router.POST("/register", handler.Register)
		body := `{"first_name":"Ada","last_name":"Lovelace","email":"ada@example.com","password":"password123"}`
		requireStatus(t, perform(router, http.MethodPost, "/register", body), http.StatusCreated)
		requireStatus(t, perform(router, http.MethodPost, "/register", `{}`), http.StatusBadRequest)
	})
	t.Run("login and invalid credentials", func(t *testing.T) {
		hash, err := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		repo := &fakeUserRepository{User: &models.User{BaseModel: models.BaseModel{ID: uuid.New()}, Email: "ada@example.com", PasswordHash: string(hash)}}
		refreshTokens := &fakeRefreshTokenRepository{}
		handler := handlers.NewAuthHandler(services.NewUserService(repo, refreshTokens, secret, time.Hour, 30*24*time.Hour))
		router := gin.New()
		router.POST("/login", handler.Login)
		router.POST("/refresh", handler.Refresh)
		router.POST("/logout", handler.Logout)
		requireStatus(t, perform(router, http.MethodPost, "/login", `{"email":"ada@example.com","password":"password123"}`), http.StatusOK)
		requireStatus(t, perform(router, http.MethodPost, "/login", `{"email":"ada@example.com","password":"wrongpass"}`), http.StatusUnauthorized)

		login := perform(router, http.MethodPost, "/login", `{"email":"ada@example.com","password":"password123"}`)
		var tokens services.AuthResponse
		if err := json.Unmarshal(login.Body.Bytes(), &tokens); err != nil {
			t.Fatal(err)
		}
		refreshBody := `{"refresh_token":"` + tokens.RefreshToken + `"}`
		refreshed := perform(router, http.MethodPost, "/refresh", refreshBody)
		requireStatus(t, refreshed, http.StatusOK)
		var rotatedTokens services.AuthResponse
		if err := json.Unmarshal(refreshed.Body.Bytes(), &rotatedTokens); err != nil {
			t.Fatal(err)
		}
		requireStatus(t, perform(router, http.MethodPost, "/refresh", refreshBody), http.StatusUnauthorized)
		rotatedRefreshBody := `{"refresh_token":"` + rotatedTokens.RefreshToken + `"}`
		requireStatus(t, perform(router, http.MethodPost, "/logout", rotatedRefreshBody), http.StatusNoContent)
		requireStatus(t, perform(router, http.MethodPost, "/refresh", rotatedRefreshBody), http.StatusUnauthorized)
	})
}

func TestUserSettingsHandler(t *testing.T) {
	userID := uuid.New()
	repository := &fakeUserSettingsRepository{Settings: &models.UserSettings{
		BaseModel:    models.BaseModel{ID: uuid.New()},
		UserID:       userID,
		Currency:     models.GBP,
		DateFormat:   models.DDMMYYYY,
		NumberFormat: models.CommaDecimal,
		WeekStartsOn: models.Monday,
	}}
	handler := handlers.NewUserSettingsHandler(services.NewUserSettingsService(repository))
	router := authenticatedRouter(userID)
	router.GET("/settings", handler.Get)
	router.PUT("/settings", handler.Update)

	requireStatus(t, perform(router, http.MethodGet, "/settings", ""), http.StatusOK)
	body := `{"currency":"EUR","date_format":"YYYY-MM-DD","number_format":"dot_decimal","week_starts_on":"Sunday"}`
	requireStatus(t, perform(router, http.MethodPut, "/settings", body), http.StatusOK)
	requireStatus(t, perform(router, http.MethodPut, "/settings", `{}`), http.StatusBadRequest)
}
