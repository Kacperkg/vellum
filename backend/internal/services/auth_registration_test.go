package services_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
	"golang.org/x/crypto/bcrypt"
)

func TestRegistrationPersistenceBoundary(t *testing.T) {
	request := services.RegisterRequest{FirstName: "Registration", LastName: "Test", Email: "registration@example.test", Password: "registration-password"}
	t.Run("prepares credentials and persists one complete aggregate", func(t *testing.T) {
		users := &fakeUserRepository{}
		response, err := services.NewUserService(users, &fakeRefreshTokenRepository{}, "test-secret", time.Hour, 24*time.Hour).Register(request)
		if err != nil || response == nil {
			t.Fatalf("registration error = %v", err)
		}
		if users.Created == nil || users.CreatedSettings == nil || users.CreatedRefreshToken == nil {
			t.Fatal("registration did not supply the complete persistence aggregate")
		}
		id := users.Created.ID
		if id == uuid.Nil || response.User.ID != id || users.CreatedSettings.UserID != id || users.CreatedRefreshToken.UserID != id {
			t.Fatal("registration identities do not match")
		}
		if err := bcrypt.CompareHashAndPassword([]byte(users.Created.PasswordHash), []byte(request.Password)); err != nil {
			t.Fatal("registration did not hash the password")
		}
		hash := sha256.Sum256([]byte(response.RefreshToken))
		if users.CreatedRefreshToken.TokenHash != hex.EncodeToString(hash[:]) || response.RefreshToken == "" {
			t.Fatal("persisted refresh hash does not match the returned token")
		}
	})
	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "persistence failure returns no credentials", err: errors.New("persistence failed")},
		{name: "concurrent email collision is a conflict", err: repository.ErrConflict, want: services.ErrUserEmailExists},
	} {
		t.Run(test.name, func(t *testing.T) {
			users := &fakeUserRepository{RegistrationErr: test.err}
			response, err := services.NewUserService(users, &fakeRefreshTokenRepository{}, "test-secret", time.Hour, 24*time.Hour).Register(request)
			if response != nil || err == nil || (test.want != nil && !errors.Is(err, test.want)) {
				t.Fatalf("response present = %v, error = %v", response != nil, err)
			}
			if users.Created != nil || users.CreatedSettings != nil || users.CreatedRefreshToken != nil {
				t.Fatal("failed registration exposed persisted aggregate state")
			}
		})
	}
}
