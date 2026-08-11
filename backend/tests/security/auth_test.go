package security_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/middleware"
)

func TestJWT(t *testing.T) {
	userID := uuid.New()
	token, err := auth.GenerateToken(userID, "secret", time.Hour)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	claims, err := auth.ValidateToken(token, "secret")
	if err != nil || claims.UserID != userID {
		t.Fatalf("ValidateToken() claims = %#v, error = %v", claims, err)
	}
	if _, err := auth.ValidateToken(token, "wrong-secret"); err == nil {
		t.Fatal("ValidateToken() accepted the wrong secret")
	}

	expired, err := auth.GenerateToken(userID, "secret", -time.Minute)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if _, err := auth.ValidateToken(expired, "secret"); err == nil {
		t.Fatal("ValidateToken() accepted an expired token")
	}
}

func TestAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	token, err := auth.GenerateToken(userID, "secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(middleware.AuthMiddleware("secret"))
	router.GET("/protected", func(c *gin.Context) {
		value, exists := c.Get("userID")
		if !exists || value != userID {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.Status(http.StatusNoContent)
	})

	tests := []struct {
		name          string
		authorization string
		wantStatus    int
	}{
		{"valid", "Bearer " + token, http.StatusNoContent},
		{"missing", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic " + token, http.StatusUnauthorized},
		{"invalid token", "Bearer invalid", http.StatusUnauthorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if test.authorization != "" {
				request.Header.Set("Authorization", test.authorization)
			}
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body = %s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
}
