package auth_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
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

func TestJWTSessionFamilyClaim(t *testing.T) {
	userID, familyID := uuid.New(), uuid.New()
	token, err := auth.GenerateSessionToken(userID, "secret", time.Hour, 7, familyID)
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.ValidateToken(token, "secret")
	if err != nil || claims.UserID != userID || claims.AuthVersion != 7 || claims.SessionID == nil || *claims.SessionID != familyID {
		t.Fatal("session JWT did not preserve identity, version and family")
	}
}
