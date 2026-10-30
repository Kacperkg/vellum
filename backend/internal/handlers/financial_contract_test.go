package handlers_test

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/money"
	"github.com/kacperkg/vellum/internal/services"
)

func TestFinancialHTTPMoneyAndCalendarContract(t *testing.T) {
	userID, accountID, categoryID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeTransactionRepository{}
	service := services.NewTransactionService(repo, &fakeAccountRepository{Account: &models.Account{UserID: userID}}, &fakeCategoryRepository{Category: &models.Category{BaseModel: models.BaseModel{ID: categoryID}}})
	handler := handlers.NewTransactionHandler(service)
	router := authenticatedRouter(userID)
	router.POST("/transactions", handler.Create)
	router.GET("/range", handler.ListByDateRange)
	payload := func(amount, date string) string {
		return fmt.Sprintf(`{"name":"Entry","amount":%s,"account_id":%q,"category_id":%q,"date":%q,"frequency":"one-off","type":"expense"}`, amount, accountID, categoryID, date)
	}
	for _, amount := range []string{"9007199254740992", "9223372036854775807", "9223372036854775808", "1.5", "0", "-1"} {
		requireStatus(t, perform(router, http.MethodPost, "/transactions", payload(amount, "2026-11-01")), http.StatusBadRequest)
		if repo.Created != nil {
			t.Fatal("rejected input persisted")
		}
	}
	for _, input := range []string{"2026-11-01", "2026-11-01T00:30:00+14:00", "2026-11-01T23:30:00-12:00"} {
		recorder := perform(router, http.MethodPost, "/transactions", payload("9007199254740991", input))
		requireStatus(t, recorder, http.StatusCreated)
		var response struct {
			Amount json.Number `json:"amount"`
			Date   string      `json:"date"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if response.Amount.String() != "9007199254740991" || response.Date != "2026-11-01T00:00:00Z" {
			t.Fatalf("wire shape/value=%s", recorder.Body.String())
		}
	}
	for _, date := range []string{"2026-11-31", "2026-11-01T00:30:00", "2026-11-01T00:30:00+24:00"} {
		requireStatus(t, perform(router, http.MethodPost, "/transactions", payload("1", date)), http.StatusBadRequest)
	}
	requireStatus(t, perform(router, http.MethodGet, "/range?start_date=2026-11-01&end_date=2026-11-30", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/range?start_date=2026-12-01&end_date=2026-11-30", ""), http.StatusBadRequest)
}

func TestFinancialHTTPAccountOverflowAndOwnership(t *testing.T) {
	userID, id := uuid.New(), uuid.New()
	account := &models.Account{BaseModel: models.BaseModel{ID: id}, UserID: userID, Balance: -money.Max, AllocatedAmount: 1}
	repo := &fakeAccountRepository{Account: account}
	handler := handlers.NewAccountHandler(services.NewAccountService(repo))
	router := authenticatedRouter(userID)
	router.GET("/accounts/:id", handler.GetByID)
	router.PUT("/accounts/:id", handler.Update)
	router.DELETE("/accounts/:id", handler.Delete)
	requireStatus(t, perform(router, http.MethodGet, "/accounts/"+id.String(), ""), http.StatusBadRequest)
	// Real repositories return identity with a range error so repair validators can
	// check ownership. A foreign account must still be indistinguishable from missing.
	repo.Err = money.ErrRange
	account.UserID = uuid.New()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		requireStatus(t, perform(router, method, "/accounts/"+id.String(), `{"name":"change"}`), http.StatusNotFound)
	}
}

func TestFinancialHTTPLegacyScalars(t *testing.T) {
	userID, id := uuid.New(), uuid.New()
	router := authenticatedRouter(userID)
	budget := &models.Budget{BaseModel: models.BaseModel{ID: id}, UserID: userID, AllocatedAmount: math.MaxInt64}
	budgets := handlers.NewBudgetHandler(services.NewBudgetService(&fakeBudgetRepository{Budget: budget, Budgets: []*models.Budget{budget}}, &fakeCategoryRepository{}))
	router.GET("/budgets", budgets.ListByUser)
	router.GET("/budgets/:id", budgets.FindByID)
	goal := &models.Goal{BaseModel: models.BaseModel{ID: id}, UserID: userID, TargetAmount: math.MaxInt64}
	goals := handlers.NewGoalHandler(services.NewGoalService(&fakeGoalRepository{Goal: goal, Goals: []*models.Goal{goal}}, &fakeAccountRepository{}))
	router.GET("/goals", goals.ListByUser)
	router.GET("/goals/:id", goals.GetByID)
	transfer := &models.Transfer{BaseModel: models.BaseModel{ID: id}, UserID: userID, Amount: math.MaxInt64}
	transfers := handlers.NewTransferHandler(services.NewTransferService(&fakeTransferRepository{transfer: transfer}, &fakeAccountRepository{}))
	router.GET("/transfers", transfers.ListByUser)
	router.GET("/transfers/:id", transfers.GetByID)
	for _, path := range []string{"/budgets", "/goals", "/transfers"} {
		requireStatus(t, perform(router, http.MethodGet, path, ""), http.StatusBadRequest)
		requireStatus(t, perform(router, http.MethodGet, path+"/"+id.String(), ""), http.StatusBadRequest)
	}
}
