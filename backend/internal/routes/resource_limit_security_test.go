package routes_test

import (
	"bufio"
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
	"github.com/jackc/pgx/v5/pgconn"
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

type resourceFamilyRepository struct {
	*fakeRefreshTokenRepository
	block   bool
	entered chan time.Duration
}

func (r *resourceFamilyRepository) HasActiveFamily(ctx context.Context, userID uuid.UUID, version int64, familyID uuid.UUID, now time.Time) (bool, error) {
	if r.block {
		deadline, _ := ctx.Deadline()
		r.entered <- time.Until(deadline)
		<-ctx.Done()
		return false, ctx.Err()
	}
	return r.fakeRefreshTokenRepository.HasActiveFamily(ctx, userID, version, familyID, now)
}

func TestResourceSessionFamilyValidationUsesRequestBudget(t *testing.T) {
	id, family := uuid.New(), uuid.New()
	const version = 2
	refresh := &resourceFamilyRepository{
		fakeRefreshTokenRepository: &fakeRefreshTokenRepository{Token: &models.RefreshToken{
			UserID: id, AuthVersion: version, FamilyID: &family, ExpiresAt: time.Now().Add(time.Hour),
		}},
		entered: make(chan time.Duration, 1),
	}
	user := &models.User{BaseModel: models.BaseModel{ID: id}, AuthVersion: version}
	set := testHandlerSet(id)
	set.Auth = handlers.NewAuthHandler(services.NewUserService(&fakeUserRepository{User: user}, refresh, "test-secret", time.Hour, time.Hour))
	router, err := routes.NewRouter(securityConfig(), set)
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.GenerateSessionToken(id, "test-secret", time.Hour, version, family)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	for _, tc := range []struct {
		name   string
		cancel bool
		want   int
	}{
		{"client cancellation", true, 408},
		{"session validation deadline", false, 504},
	} {
		t.Run(tc.name, func(t *testing.T) {
			refresh.block = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			response := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				router.ServeHTTP(response, request.Clone(ctx))
			}()
			select {
			case remaining := <-refresh.entered:
				if remaining <= 0 || remaining > 3*time.Second {
					cancel()
					<-done
					t.Fatalf("session validation budget=%v", remaining)
				}
			case <-done:
				t.Fatalf("session lookup was skipped: status %d", response.Code)
			case <-time.After(5 * time.Second):
				t.Fatal("session lookup did not start")
			}
			if tc.cancel {
				cancel()
			}
			<-done
			if response.Code != tc.want {
				t.Fatalf("session validation status=%d body=%s", response.Code, response.Body)
			}
		})
	}
	refresh.block = false
	check := func(want int) {
		t.Helper()
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request.Clone(context.Background()))
		if response.Code != want {
			t.Fatalf("session status=%d want=%d body=%s", response.Code, want, response.Body)
		}
	}
	check(200)
	refresh.Err = &pgconn.PgError{Code: "55P03", Message: "private database detail"}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request.Clone(context.Background()))
	if response.Code != 503 || response.Body.String() != `{"error":"session validation unavailable"}` {
		t.Fatalf("session lookup failure status=%d body=%s", response.Code, response.Body)
	}
	refresh.Err = nil
	check(200)
	revoked := time.Now()
	refresh.Token.RevokedAt = &revoked
	check(401)
	refresh.Token.RevokedAt = nil
	user.AuthVersion++
	check(401)
}

func TestResourceWriteBodyBoundaries(t *testing.T) {
	id := uuid.New()
	accounts := &fakeAccountRepository{}
	set := testHandlerSet(id)
	set.Accounts = handlers.NewAccountHandler(services.NewAccountService(accounts))
	router, err := routes.NewRouter(securityConfig(), set)
	if err != nil {
		t.Fatal(err)
	}
	const valid = `{"name":"Savings"}`
	padded := func(size int) string { return valid + strings.Repeat(" ", size-len(valid)) }
	for _, tc := range []struct {
		name, method, body, mediaType string
		chunked                       bool
		want                          int
	}{
		{"below limit", http.MethodPost, padded(middleware.MaxResourceBodyBytes - 1), "application/json", false, 201},
		{"at limit", http.MethodPost, padded(middleware.MaxResourceBodyBytes), "application/json; charset=utf-8", true, 201},
		{"above limit", http.MethodPost, padded(middleware.MaxResourceBodyBytes + 1), "application/json", true, 413},
		{"extra JSON", http.MethodPost, valid + `{}`, "application/json", true, 400},
		{"trailing junk", http.MethodPost, valid + "invalid", "application/json", false, 400},
		{"unsupported content type", http.MethodPost, valid, "text/plain", false, 415},
		{"unsupported method", http.MethodPatch, valid, "application/json", false, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accounts.Created = nil
			request := httptest.NewRequest(tc.method, "/api/accounts", strings.NewReader(tc.body))
			if tc.chunked {
				raw := fmt.Sprintf("%s /api/accounts HTTP/1.1\r\nHost: example.test\r\nTransfer-Encoding: chunked\r\n\r\n%x\r\n%s\r\n0\r\n\r\n", tc.method, len(tc.body), tc.body)
				request, err = http.ReadRequest(bufio.NewReader(strings.NewReader(raw)))
				if err != nil {
					t.Fatal(err)
				}
				defer request.Body.Close()
			}
			request.Header.Set("Authorization", resourceToken(t, id))
			request.Header.Set("Content-Type", tc.mediaType)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want || (accounts.Created != nil) != (tc.want == 201) {
				t.Fatalf("status=%d created=%t body=%s", response.Code, accounts.Created != nil, response.Body)
			}
		})
	}
}

func TestResourceCalendarRangeLimits(t *testing.T) {
	id := uuid.New()
	router, err := routes.NewRouter(securityConfig(), testHandlerSet(id))
	if err != nil {
		t.Fatal(err)
	}
	headers := http.Header{"Authorization": {resourceToken(t, id)}}
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"start_date=2024-01-01&end_date=2025-01-01", 200},
		{"start_date=2024-01-01T00:00:00%2B14:00&end_date=2025-01-01T23:59:59-12:00", 200},
		{"start_date=2024-01-01&end_date=2025-01-02", 400},
		{"start_date=2025-01-02&end_date=2025-01-01", 400},
		{"start_date=2024-02-30&end_date=2024-03-01", 400},
		{"start_date=2024-01-01&start_date=2024-02-01&end_date=2024-03-01", 400},
	} {
		response := chainRequest(router, http.MethodGet, "/api/transactions/date-range?"+tc.query, "", "192.0.2.1:1", headers)
		if response.Code != tc.want {
			t.Errorf("%s: status=%d want=%d body=%s", tc.query, response.Code, tc.want, response.Body)
		}
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
