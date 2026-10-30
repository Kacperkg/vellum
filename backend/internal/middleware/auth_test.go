package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/middleware"
)

func TestAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	userID := uuid.New()
	token, err := auth.GenerateToken(userID, "secret", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	router := gin.New()
	router.Use(middleware.AuthMiddleware("secret", func(context.Context, uuid.UUID, int64, *uuid.UUID) bool { return true }))
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
