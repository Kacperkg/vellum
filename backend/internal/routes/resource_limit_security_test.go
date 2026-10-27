package routes_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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

type resourceAuthRepository struct {
	*fakeUserRepository
	entered chan struct{}
	release <-chan struct{}
	calls   atomic.Int32
}

func (r *resourceAuthRepository) FindByIDContext(ctx context.Context, id uuid.UUID) (*models.User, error) {
	r.calls.Add(1)
	if r.entered != nil {
		r.entered <- struct{}{}
		select {
		case <-r.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return r.fakeUserRepository.FindByIDContext(ctx, id)
}
func resourceToken(t *testing.T, id uuid.UUID) string {
	t.Helper()
	token, err := auth.GenerateToken(id, "test-secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return "Bearer " + token
}
func TestResourcePolicyPreservesOverlappingFinanceHydrationAndOnboarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	release := make(chan struct{})
	users := &resourceAuthRepository{fakeUserRepository: &fakeUserRepository{User: &models.User{BaseModel: models.BaseModel{ID: id}}}, entered: make(chan struct{}, 14), release: release}
	set := testHandlerSet(id)
	set.Auth = handlers.NewAuthHandler(services.NewUserService(users, &fakeRefreshTokenRepository{}, "test-secret", time.Hour, time.Hour))
	accounts := &fakeAccountRepository{Accounts: []*models.Account{}}
	set.Accounts = handlers.NewAccountHandler(services.NewAccountService(accounts))
	router, err := routes.NewRouter(securityConfig(), set)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Authorization": {resourceToken(t, id)}}
	// FinanceDataContext performs seven GETs together. Hold auth-version lookups
	// until both complete hydration bursts enter through the production router.
	paths := []string{"accounts", "budgets", "categories", "goals", "transactions", "settings", "transfers"}
	responses := make(chan *httptest.ResponseRecorder, 14)
	for burst := 0; burst < 2; burst++ {
		for _, path := range paths {
			go func(path string) {
				responses <- chainRequest(router, http.MethodGet, "/api/"+path, "", "192.0.2.1:1", headers)
			}(path)
		}
	}
	for range 14 {
		select {
		case <-users.entered:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("two hydration bursts were not admitted")
		}
	}
	close(release)
	for range 14 {
		response := <-responses
		if response.Code != 200 {
			t.Fatalf("hydration status=%d body=%s", response.Code, response.Body)
		}
	}
	response := chainRequest(router, http.MethodPost, "/api/accounts", `{"name":"Current account"}`, "192.0.2.1:1", headers)
	if response.Code != 201 || accounts.Created == nil || accounts.Created.Name != "Current account" {
		t.Fatalf("onboarding status=%d body=%s", response.Code, response.Body)
	}
	response = chainRequest(router, http.MethodPut, "/api/settings", `{"currency":"GBP","date_format":"DD/MM/YYYY","number_format":"comma_decimal","week_starts_on":"Monday"}`, "192.0.2.1:1", headers)
	if response.Code != 200 {
		t.Fatalf("settings status=%d body=%s", response.Code, response.Body)
	}
}
func TestResourceSourceOverloadPrecedesDatabaseWorkAndPreservesPublicAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	release := make(chan struct{})
	users := &resourceAuthRepository{fakeUserRepository: &fakeUserRepository{User: &models.User{BaseModel: models.BaseModel{ID: id}}}, entered: make(chan struct{}, 24), release: release}
	set := testHandlerSet(id)
	set.Auth = handlers.NewAuthHandler(services.NewUserService(users, &fakeRefreshTokenRepository{}, "test-secret", time.Hour, time.Hour))
	set.PasswordReset = handlers.NewPasswordResetHandler(nil)
	router, err := routes.NewRouter(securityConfig(), set)
	if err != nil {
		t.Fatal(err)
	}
	token := resourceToken(t, id)
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			chainRequest(router, http.MethodGet, "/api/accounts", "", "192.0.2.1:1", http.Header{"Authorization": {token}, "X-Forwarded-For": {fmt.Sprintf("198.51.100.%d", i+1)}})
		}(i)
	}
	defer func() { close(release); wg.Wait() }()
	for range 24 {
		select {
		case <-users.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("source requests did not enter auth-version validation")
		}
	}
	response := chainRequest(router, http.MethodGet, "/api/accounts", "", "192.0.2.1:1", http.Header{"Authorization": {token}, "X-Forwarded-For": {"203.0.113.99"}})
	if response.Code != 429 || response.Header().Get("Retry-After") != "1" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("overload status=%d headers=%v", response.Code, response.Header())
	}
	if users.calls.Load() != 24 {
		t.Fatalf("rejected request queried auth database: calls=%d", users.calls.Load())
	}
	for _, test := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodGet, "/ping", "", 200}, {http.MethodPost, "/login", `{}`, 400},
		{http.MethodPost, "/forgot-password", `{"email":"unknown@example.com"}`, 200}, {http.MethodPost, "/reset-password", `{}`, 400},
	} {
		response := chainRequest(router, test.method, test.path, test.body, "192.0.2.1:1", nil)
		if response.Code != test.want {
			t.Errorf("public %s status=%d want=%d", test.path, response.Code, test.want)
		}
	}
}
func TestResourceUserQuotaSurvivesSourceChanges(t *testing.T) {
	id := uuid.New()
	router, err := routes.NewRouter(securityConfig(), testHandlerSet(id))
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Authorization": {resourceToken(t, id)}}
	for i := range 241 {
		response := chainRequest(router, http.MethodGet, "/api/accounts", "", fmt.Sprintf("192.0.2.%d:1", i%250+1), headers)
		want := 200
		if i == 240 {
			want = 429
		}
		if response.Code != want {
			t.Fatalf("request %d status=%d want=%d", i+1, response.Code, want)
		}
		if i == 240 && (response.Header().Get("Retry-After") == "" || response.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("user quota rejection omitted retry/no-store guidance")
		}
	}
}
func TestResourceMutationBodiesAreRejectedBeforeSideEffects(t *testing.T) {
	id := uuid.New()
	accounts := &fakeAccountRepository{}
	set := testHandlerSet(id)
	set.Accounts = handlers.NewAccountHandler(services.NewAccountService(accounts))
	router, err := routes.NewRouter(securityConfig(), set)
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Authorization": {resourceToken(t, id)}}
	for _, path := range []string{"accounts", "budgets", "goals", "transactions", "transfers"} {
		// Ignored trailing bytes still count when Content-Length is unavailable.
		request := httptest.NewRequest(http.MethodPost, "/api/"+path, strings.NewReader(`{"name":"valid"}`+strings.Repeat(" ", middleware.MaxResourceBodyBytes)))
		request.ContentLength = -1
		request.RemoteAddr = "192.0.2.1:1"
		request.Header = headers.Clone()
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != 413 {
			t.Fatalf("%s oversized status=%d body=%s", path, response.Code, response.Body)
		}
	}
	if accounts.Created != nil {
		t.Fatal("oversized request committed an account")
	}
	response := chainRequest(router, http.MethodGet, "/api/transactions?ignored="+strings.Repeat("x", middleware.MaxResourceQueryBytes), "", "192.0.2.1:1", headers)
	if response.Code != 400 {
		t.Fatalf("oversized query status=%d", response.Code)
	}
}
func TestAuthVersionTimeoutUsesRequestDeadlineWithoutInvalidatingSession(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	limiter := middleware.NewResourceLimiter()
	router := gin.New()
	var handled atomic.Bool
	var validations atomic.Int32
	router.GET("/api/test", limiter.SourceHandler(), middleware.AuthMiddleware("test-secret", func(ctx context.Context, _ uuid.UUID, _ int64) bool {
		if validations.Add(1) == 1 {
			<-ctx.Done()
			return false
		}
		return true
	}), limiter.UserHandler(), func(c *gin.Context) { handled.Store(true); c.Status(200) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/test", nil).WithContext(ctx)
	request.Header.Set("Authorization", resourceToken(t, id))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 504 || handled.Load() || response.Body.String() != `{"error":"request timed out"}` {
		t.Fatalf("deadline status=%d handled=%v body=%s", response.Code, handled.Load(), response.Body)
	}
	response = chainRequest(router, http.MethodGet, "/api/test", "", "192.0.2.1:1", http.Header{"Authorization": {resourceToken(t, id)}})
	if response.Code != http.StatusOK || !handled.Load() {
		t.Fatalf("session after timeout status=%d handled=%v", response.Code, handled.Load())
	}
}
func TestResourceDatabaseFailureDoesNotExposeInternals(t *testing.T) {
	id := uuid.New()
	set := testHandlerSet(id)
	set.Accounts = handlers.NewAccountHandler(services.NewAccountService(&fakeAccountRepository{Err: errors.New("postgres connection secret detail")}))
	router, err := routes.NewRouter(securityConfig(), set)
	if err != nil {
		t.Fatal(err)
	}
	response := chainRequest(router, http.MethodGet, "/api/accounts", "", "192.0.2.1:1", http.Header{"Authorization": {resourceToken(t, id)}})
	if response.Code != 500 || response.Body.String() != `{"error":"internal server error"}` {
		t.Fatalf("database failure status=%d body=%s", response.Code, response.Body)
	}
}
