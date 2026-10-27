package routes_test

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRecoveryPublicRoutesGenericResponseRateLimitsAndLogs(t *testing.T) {
	cfg := securityConfig()
	cfg.ForgotPasswordRateLimit.Requests = 2
	cfg.ForgotPasswordRateLimit.Window = time.Hour
	set := testHandlerSet(uuid.New())
	set.PasswordReset = handlers.NewPasswordResetHandler(nil)
	router, err := routes.NewRouter(cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	call := func(path, body string) *httptest.ResponseRecorder {
		t.Helper()
		return chainRequest(router, http.MethodPost, path, body, "192.0.2.5:1234", nil)
	}
	first := call("/forgot-password", `{"email":"existing@example.com"}`)
	unknown := call("/forgot-password", `{"email":"unknown@example.com"}`)
	if first.Code != 200 || unknown.Code != 200 || first.Body.String() != unknown.Body.String() || !strings.Contains(first.Body.String(), services.RecoveryMessage) {
		t.Fatal("enumerable recovery response")
	}
	if first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("missing recovery headers")
	}
	if got := call("/forgot-password", `{"email":"existing@example.com"}`).Code; got != 200 {
		t.Fatalf("second status=%d", got)
	}
	if got := call("/forgot-password", `{"email":"existing@example.com"}`).Code; got != 429 {
		t.Fatalf("rate limit status=%d", got)
	}
	if got := call("/forgot-password", `{"email":"malformed"}`).Code; got != 400 {
		t.Fatalf("validation status=%d", got)
	}
	secret := "SensitiveBearerDoNotLog"
	reset := call("/reset-password?token="+secret, `{"token":"`+strings.Repeat("a", 43)+`","new_password":"new-password"}`)
	if reset.Code != 400 {
		t.Fatalf("invalid reset=%d", reset.Code)
	}
	if strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "example.com") || strings.Contains(logs.String(), "new-password") {
		t.Fatal("sensitive request data in logs")
	}
	router.GET("/panic", func(c *gin.Context) { panic("secret panic payload") })
	panicResponse := chainRequest(router, http.MethodGet, "/panic?token="+secret, "", "192.0.2.5:1", http.Header{"Authorization": []string{"Bearer " + secret}})
	if panicResponse.Code != 500 || strings.Contains(logs.String(), secret) || strings.Contains(logs.String(), "secret panic payload") {
		t.Fatal("panic leaked sensitive request data")
	}
	preflight := chainRequest(router, http.MethodOptions, "/reset-password", "", "192.0.2.5:1", http.Header{"Origin": []string{"http://localhost:5173"}, "Access-Control-Request-Method": []string{"POST"}, "Access-Control-Request-Headers": []string{"content-type"}})
	if preflight.Code != 204 {
		t.Fatalf("preflight status=%d", preflight.Code)
	}
}
func TestAuthVersionRouteInvalidationAndDatabaseFailures(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	user := &models.User{BaseModel: models.BaseModel{ID: id}, AuthVersion: 0}
	users := &fakeUserRepository{User: user}
	set := testHandlerSet(id)
	set.Auth = handlers.NewAuthHandler(services.NewUserService(users, &fakeRefreshTokenRepository{}, "secret", time.Hour, time.Hour))
	cfg := securityConfig()
	cfg.JWTSecret = "secret"
	router, err := routes.NewRouter(cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	token, err := auth.GenerateToken(id, "secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := func(token string) int {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(recorder, req)
		return recorder.Code
	}
	if request(token) != 200 {
		t.Fatal("version0 session rejected")
	}
	user.AuthVersion++
	if request(token) != 401 {
		t.Fatal("old access token survived reset")
	}
	fresh, err := auth.GenerateToken(id, "secret", time.Hour, user.AuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	if request(fresh) != 200 {
		t.Fatal("new access token rejected")
	}
	users.Err = context.DeadlineExceeded
	if request(fresh) != 401 {
		t.Fatal("database lookup failure did not fail closed")
	}
}
