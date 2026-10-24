package handlers_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
)

func TestRegistrationPersistenceHTTPFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{name: "concurrent duplicate email", err: repository.ErrConflict, status: http.StatusConflict},
		{name: "transaction failure", err: errors.New("persistence failed"), status: http.StatusInternalServerError},
	} {
		t.Run(test.name, func(t *testing.T) {
			users := &fakeUserRepository{RegistrationErr: test.err}
			handler := handlers.NewAuthHandler(services.NewUserService(users, &fakeRefreshTokenRepository{}, "test-secret", time.Hour, 24*time.Hour))
			router := gin.New()
			router.POST("/register", handler.Register)
			response := perform(router, http.MethodPost, "/register", `{"first_name":"Registration","last_name":"Test","email":"registration@example.test","password":"registration-password"}`)
			requireStatus(t, response, test.status)
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["error"] == nil || body["access_token"] != nil || body["refresh_token"] != nil || body["user"] != nil {
				t.Fatal("failed registration returned credentials or omitted its error")
			}
		})
	}
}
