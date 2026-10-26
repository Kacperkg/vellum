package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
)

func TestAccessJWTRequiresExpirationAndPinnedAlgorithm(t *testing.T) {
	id := uuid.New()
	for _, test := range []struct {
		name      string
		method    jwt.SigningMethod
		expires   bool
		wantValid bool
	}{
		{"legacy HS256", jwt.SigningMethodHS256, true, true},
		{"missing expiry", jwt.SigningMethodHS256, false, false},
		{"HS384 rejected", jwt.SigningMethodHS384, true, false},
		{"HS512 rejected", jwt.SigningMethodHS512, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			claims := jwt.MapClaims{"user_id": id.String()}
			if test.expires {
				claims["exp"] = time.Now().Add(time.Hour).Unix()
			}
			token, err := jwt.NewWithClaims(test.method, claims).SignedString([]byte("test-jwt-secret"))
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := auth.ValidateToken(token, "test-jwt-secret")
			if test.wantValid {
				if err != nil || parsed == nil || parsed.UserID != id || parsed.AuthVersion != 0 {
					t.Fatalf("legacy claims rejected or changed: %v", err)
				}
			} else if err == nil {
				t.Fatal("JWT without required security properties accepted")
			}
		})
	}
}
