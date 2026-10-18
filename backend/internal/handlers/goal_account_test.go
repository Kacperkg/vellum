package handlers_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
)

func TestGoalHandlerAccountLinkContract(t *testing.T) {
	userID, accountID, goalID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeGoalRepository{Goal: &models.Goal{BaseModel: models.BaseModel{ID: goalID}, UserID: userID, AccountID: &accountID}}
	accounts := &fakeAccountRepository{Account: &models.Account{UserID: userID}}
	handler := handlers.NewGoalHandler(services.NewGoalService(repo, accounts))
	router := authenticatedRouter(userID)
	router.POST("/goals", handler.Create)
	router.PUT("/goals/:id", handler.Update)
	for _, accountJSON := range []string{`"` + accountID.String() + `"`, `null`} {
		body := `{"name":"Trip","target_amount":120000,"account_id":` + accountJSON + `}`
		created := perform(router, http.MethodPost, "/goals", body)
		requireStatus(t, created, http.StatusCreated)
		var result map[string]any
		if err := json.Unmarshal(created.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		value, present := result["account_id"]
		if !present || (accountJSON == `null` && value != nil) || (accountJSON != `null` && value != accountID.String()) {
			t.Fatalf("account_id response=%#v", result)
		}
		requireStatus(t, perform(router, http.MethodPut, "/goals/"+goalID.String(), body), http.StatusOK)
	}
	if repo.Updated.AccountID != nil {
		t.Fatal("null did not unlink")
	}
	requireStatus(t, perform(router, http.MethodPost, "/goals", `{"name":"Trip","target_amount":120000,"account_id":"bad"}`), http.StatusBadRequest)
	accounts.Account.UserID = uuid.New()
	requireStatus(t, perform(router, http.MethodPost, "/goals", `{"name":"Trip","target_amount":120000,"account_id":"`+accountID.String()+`"}`), http.StatusNotFound)
}
