package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/middleware"
)

func TestLegacyTokenOnlyValidBeforeVersionChange(t *testing.T) {
	id := uuid.New()
	version := int64(0)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"user_id": id.String(), "exp": time.Now().Add(time.Hour).Unix()}).SignedString([]byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(middleware.AuthMiddleware("secret", func(ctx context.Context, userID uuid.UUID, claimVersion int64) bool {
		return ctx != nil && id == userID && version == claimVersion
	}))
	router.GET("/api", func(c *gin.Context) { c.Status(204) })
	for _, want := range []int{204, 401} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("legacy version%d got%d want%d", version, rec.Code, want)
		}
		version++
	}
}
