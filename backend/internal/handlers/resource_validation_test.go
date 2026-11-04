package handlers_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
)

func TestAuthenticatedBindingErrorsAreSafe(t *testing.T) {
	userID := uuid.New()
	repo := &fakeAccountRepository{}
	handler := handlers.NewAccountHandler(services.NewAccountService(repo))
	router := authenticatedRouter(userID)
	router.POST("/accounts", handler.Create)
	for _, body := range []string{`{"name":42}`, `{"name":"` + strings.Repeat("a", 201) + `"}`, `{"name":"private value"`, `{"name":""}`} {
		response := perform(router, http.MethodPost, "/accounts", body)
		requireStatus(t, response, http.StatusBadRequest)
		if response.Body.String() != `{"error":"invalid request body"}` {
			t.Fatalf("unsafe binding error: %s", response.Body.String())
		}
	}
	if repo.Created != nil {
		t.Fatal("invalid request reached persistence")
	}
	response := perform(router, http.MethodPost, "/accounts", `{"name":"`+strings.Repeat("界", 200)+`"}`)
	requireStatus(t, response, http.StatusCreated)
	if repo.Created.Name != strings.Repeat("界", 200) {
		t.Fatal("boundary Unicode name was truncated")
	}
}

func TestAuthenticatedBodyLimitError(t *testing.T) {
	handler := handlers.NewAccountHandler(services.NewAccountService(&fakeAccountRepository{}))
	router := authenticatedRouter(uuid.New())
	router.Use(func(c *gin.Context) { c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10); c.Next() })
	router.POST("/accounts", handler.Create)
	requireStatus(t, perform(router, http.MethodPost, "/accounts", `{"name":"Savings"}`), http.StatusRequestEntityTooLarge)
}

func TestAuthenticatedResourceErrors(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		status  int
		message string
	}{
		{"deadline", context.DeadlineExceeded, http.StatusGatewayTimeout, "request timed out"},
		{"canceled", context.Canceled, http.StatusRequestTimeout, "request canceled"},
		{"database", errors.New("database credentials and private query"), http.StatusInternalServerError, "internal server error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := handlers.NewAccountHandler(services.NewAccountService(&fakeAccountRepository{Err: test.err}))
			router := authenticatedRouter(uuid.New())
			router.GET("/accounts", handler.ListByUser)
			response := perform(router, http.MethodGet, "/accounts", "")
			requireStatus(t, response, test.status)
			if response.Body.String() != `{"error":"`+test.message+`"}` {
				t.Fatalf("body=%s", response.Body.String())
			}
		})
	}
}

// A repository spy verifies the handler binds the actual request context to
// its service, rather than substituting a background context.
type contextAccountRepository struct {
	fakeAccountRepository
	ctx context.Context
}

func (r *contextAccountRepository) WithContext(ctx context.Context) repository.AccountRepository {
	clone := *r
	clone.ctx = ctx
	return &clone
}
func (r *contextAccountRepository) FindByUserID(uuid.UUID) ([]*models.Account, error) {
	if r.ctx == nil {
		return nil, errors.New("request context missing")
	}
	return nil, r.ctx.Err()
}
func TestResourceHandlerPropagatesCancellation(t *testing.T) {
	handler := handlers.NewAccountHandler(services.NewAccountService(&contextAccountRepository{}))
	router := authenticatedRouter(uuid.New())
	router.GET("/accounts", handler.ListByUser)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/accounts", nil).WithContext(ctx)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	requireStatus(t, recorder, http.StatusRequestTimeout)
}
