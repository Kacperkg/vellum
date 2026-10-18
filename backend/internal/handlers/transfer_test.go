package handlers_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
)

type fakeTransferRepository struct{ transfer *models.Transfer }

func (r *fakeTransferRepository) Create(value *models.Transfer) error {
	value.ID = uuid.New()
	r.transfer = value
	return nil
}
func (r *fakeTransferRepository) FindByID(id uuid.UUID) (*models.Transfer, error) {
	return r.transfer, nil
}
func (r *fakeTransferRepository) FindByUserID(uuid.UUID) ([]*models.Transfer, error) {
	if r.transfer == nil {
		return []*models.Transfer{}, nil
	}
	return []*models.Transfer{r.transfer}, nil
}
func (r *fakeTransferRepository) Update(_ uuid.UUID, value *models.Transfer) error {
	r.transfer = value
	return nil
}
func (r *fakeTransferRepository) Delete(_ uuid.UUID, _ uuid.UUID) error { r.transfer = nil; return nil }

func TestTransferHandler(t *testing.T) {
	userID, sourceID, destinationID := uuid.New(), uuid.New(), uuid.New()
	repo := &fakeTransferRepository{}
	accounts := &fakeAccountRepository{Account: &models.Account{UserID: userID}}
	handler := handlers.NewTransferHandler(services.NewTransferService(repo, accounts))
	router := authenticatedRouter(userID)
	router.POST("/transfers", handler.Create)
	router.GET("/transfers", handler.ListByUser)
	router.GET("/transfers/:id", handler.GetByID)
	router.PUT("/transfers/:id", handler.Update)
	router.DELETE("/transfers/:id", handler.Delete)
	body := `{"name":"Subscription funding","amount":12599,"from_account_id":"` + sourceID.String() + `","to_account_id":"` + destinationID.String() + `","date":"2026-10-24T12:00:00Z"}`
	requireStatus(t, perform(router, http.MethodPost, "/transfers", body), http.StatusCreated)
	if repo.transfer.Frequency != models.OneOff {
		t.Fatalf("omitted frequency=%q, want one-off", repo.transfer.Frequency)
	}
	id := repo.transfer.ID.String()
	requireStatus(t, perform(router, http.MethodGet, "/transfers", ""), http.StatusOK)
	requireStatus(t, perform(router, http.MethodGet, "/transfers/"+id, ""), http.StatusOK)
	for _, frequency := range []models.Frequency{models.Daily, models.Weekly, models.Monthly, models.Yearly, models.OneOff} {
		withFrequency := strings.TrimSuffix(body, "}") + `,"frequency":"` + string(frequency) + `"}`
		requireStatus(t, perform(router, http.MethodPut, "/transfers/"+id, withFrequency), http.StatusOK)
		response := perform(router, http.MethodGet, "/transfers/"+id, "")
		requireStatus(t, response, http.StatusOK)
		var found services.TransferResponse
		if err := json.Unmarshal(response.Body.Bytes(), &found); err != nil || found.Frequency != frequency {
			t.Fatalf("response=%s frequency=%q error=%v", response.Body.String(), found.Frequency, err)
		}
	}
	for _, frequency := range []string{"", "fortnightly", "MONTHLY", " monthly "} {
		invalid := strings.TrimSuffix(body, "}") + `,"frequency":"` + frequency + `"}`
		requireStatus(t, perform(router, http.MethodPost, "/transfers", invalid), http.StatusBadRequest)
		requireStatus(t, perform(router, http.MethodPut, "/transfers/"+id, invalid), http.StatusBadRequest)
	}
	requireStatus(t, perform(router, http.MethodPut, "/transfers/"+id, body), http.StatusOK)
	if repo.transfer.Frequency != models.OneOff {
		t.Fatalf("omitted update frequency=%q, want one-off", repo.transfer.Frequency)
	}
	withNullFrequency := strings.TrimSuffix(body, "}") + `,"frequency":null}`
	requireStatus(t, perform(router, http.MethodPut, "/transfers/"+id, withNullFrequency), http.StatusOK)
	if repo.transfer.Frequency != models.OneOff {
		t.Fatalf("null update frequency=%q, want one-off", repo.transfer.Frequency)
	}
	if repo.transfer.Amount != 12599 || !repo.transfer.Date.Equal(time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("transfer=%#v", repo.transfer)
	}
	requireStatus(t, perform(router, http.MethodPost, "/transfers", `{"amount":1}`), http.StatusBadRequest)
	requireStatus(t, perform(router, http.MethodPost, "/transfers", `{"name":"bad","amount":1.5}`), http.StatusBadRequest)
	requireStatus(t, perform(router, http.MethodGet, "/transfers/bad-id", ""), http.StatusBadRequest)
	repo.transfer.UserID = uuid.New()
	requireStatus(t, perform(router, http.MethodGet, "/transfers/"+id, ""), http.StatusNotFound)
	requireStatus(t, perform(router, http.MethodPut, "/transfers/"+id, body), http.StatusNotFound)
	requireStatus(t, perform(router, http.MethodDelete, "/transfers/"+id, ""), http.StatusNotFound)
	repo.transfer.UserID = userID
	requireStatus(t, perform(router, http.MethodDelete, "/transfers/"+id, ""), http.StatusNoContent)
	requireStatus(t, perform(router, http.MethodGet, "/transfers/"+id, ""), http.StatusNotFound)
}
