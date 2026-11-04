package repository_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
	"github.com/kacperkg/vellum/internal/middleware"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/repository"
	"github.com/kacperkg/vellum/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const familyTestSecret = "isolated-session-family-test-secret"

func familyTestService(t *testing.T, db *gorm.DB) (*services.UserService, *models.User, services.LoginRequest) {
	t.Helper()
	user := resetTestUser(t, db)
	hash, err := bcrypt.GenerateFromPassword([]byte("session-test-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user.PasswordHash = string(hash)
	if err := db.Model(user).Update("password_hash", user.PasswordHash).Error; err != nil {
		t.Fatal(err)
	}
	return services.NewUserService(repository.NewUserRepository(db), repository.NewRefreshTokenRepository(db), familyTestSecret, time.Hour, 24*time.Hour), user,
		services.LoginRequest{Email: user.Email, Password: "session-test-password"}
}

func familyLogin(t *testing.T, service *services.UserService, request services.LoginRequest) *services.AuthResponse {
	t.Helper()
	response, err := service.Login(request)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func assertFamilyAccess(t *testing.T, service *services.UserService, token string, want int) {
	t.Helper()
	router := gin.New()
	router.Use(middleware.AuthMiddleware(familyTestSecret, service.ValidateSession))
	router.GET("/protected", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != want {
		t.Fatalf("protected request status=%d, want=%d", recorder.Code, want)
	}
}

func TestSessionFamilyLogoutRevokesRotatedCredentialsOnly(t *testing.T) {
	db := resetIntegrationDB(t)
	service, user, request := familyTestService(t, db)
	initial := familyLogin(t, service, request)
	other := familyLogin(t, service, request)
	rotated, err := service.Refresh(services.RefreshRequest{RefreshToken: initial.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	latest, err := service.Refresh(services.RefreshRequest{RefreshToken: rotated.RefreshToken})
	if err != nil {
		t.Fatal(err)
	}
	// Access issued earlier in the family survives a normal rotation.
	assertFamilyAccess(t, service, initial.AccessToken, http.StatusNoContent)
	assertFamilyAccess(t, service, latest.AccessToken, http.StatusNoContent)
	// This is the credential the browser still holds while refresh responses
	// are delayed: the server has already rotated it twice.
	if err := service.Logout(services.RefreshRequest{RefreshToken: initial.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []*services.AuthResponse{initial, rotated, latest} {
		if _, err := service.Refresh(services.RefreshRequest{RefreshToken: pair.RefreshToken}); !errors.Is(err, services.ErrInvalidRefreshToken) {
			t.Fatalf("signed-out family refreshed: %v", err)
		}
		assertFamilyAccess(t, service, pair.AccessToken, http.StatusUnauthorized)
	}
	if err := service.Logout(services.RefreshRequest{RefreshToken: initial.RefreshToken}); err != nil {
		t.Fatalf("repeated logout is not idempotent: %v", err)
	}
	if err := service.Logout(services.RefreshRequest{RefreshToken: "unknown-synthetic-token"}); err != nil {
		t.Fatalf("unknown logout is not idempotent: %v", err)
	}
	assertFamilyAccess(t, service, other.AccessToken, http.StatusNoContent)
	if _, err := service.Refresh(services.RefreshRequest{RefreshToken: other.RefreshToken}); err != nil {
		t.Fatalf("unrelated login was revoked: %v", err)
	}
	var actual models.User
	if err := db.First(&actual, "id = ?", user.ID).Error; err != nil {
		t.Fatal(err)
	}
	if actual.AuthVersion != user.AuthVersion {
		t.Fatal("logout changed account-wide authentication version")
	}
}

func TestSessionFamilyLegacyTokenRotationAndPasswordReset(t *testing.T) {
	db := resetIntegrationDB(t)
	service, user, request := familyTestService(t, db)
	raw := "synthetic-legacy-" + uuid.NewString()
	hash := sha256.Sum256([]byte(raw))
	legacy := &models.RefreshToken{UserID: user.ID, TokenHash: hex.EncodeToString(hash[:]), ExpiresAt: time.Now().Add(time.Hour)}
	if err := db.Create(legacy).Error; err != nil {
		t.Fatal(err)
	}
	legacyJWT, err := auth.GenerateToken(user.ID, familyTestSecret, time.Hour, user.AuthVersion)
	if err != nil {
		t.Fatal(err)
	}
	rotated, err := service.Refresh(services.RefreshRequest{RefreshToken: raw})
	if err != nil {
		t.Fatal(err)
	}
	claims, err := auth.ValidateToken(rotated.AccessToken, familyTestSecret)
	if err != nil || claims.SessionID == nil || *claims.SessionID != legacy.ID {
		t.Fatal("legacy rotation did not bind JWT to its stable family")
	}
	if err := service.Logout(services.RefreshRequest{RefreshToken: raw}); err != nil {
		t.Fatal(err)
	}
	assertFamilyAccess(t, service, rotated.AccessToken, http.StatusUnauthorized)
	if _, err := service.Refresh(services.RefreshRequest{RefreshToken: rotated.RefreshToken}); !errors.Is(err, services.ErrInvalidRefreshToken) {
		t.Fatalf("legacy successor survived logout: %v", err)
	}
	// Pre-upgrade JWTs deliberately retain their original bounded lifetime.
	assertFamilyAccess(t, service, legacyJWT, http.StatusNoContent)
	first := familyLogin(t, service, request)
	second := familyLogin(t, service, request)
	resetHash := issueReset(t, db, user)
	if err := repository.NewPasswordResetRepository(db).Consume(context.Background(), resetHash, "new-test-hash"); err != nil {
		t.Fatal(err)
	}
	for _, pair := range []*services.AuthResponse{first, second} {
		assertFamilyAccess(t, service, pair.AccessToken, http.StatusUnauthorized)
		if _, err := service.Refresh(services.RefreshRequest{RefreshToken: pair.RefreshToken}); !errors.Is(err, services.ErrInvalidRefreshToken) {
			t.Fatalf("session family survived password reset: %v", err)
		}
	}
	assertFamilyAccess(t, service, legacyJWT, http.StatusUnauthorized)
}

func TestSessionFamilyConcurrentRotationSingleUse(t *testing.T) {
	db := resetIntegrationDB(t)
	service, _, request := familyTestService(t, db)
	initial := familyLogin(t, service, request)
	const contenders = 8
	start := make(chan struct{})
	results := make(chan error, contenders)
	for i := 0; i < contenders; i++ {
		go func() {
			<-start
			_, err := service.Refresh(services.RefreshRequest{RefreshToken: initial.RefreshToken})
			results <- err
		}()
	}
	close(start)
	successes := 0
	for i := 0; i < contenders; i++ {
		err := <-results
		if err == nil {
			successes++
		} else if !errors.Is(err, services.ErrInvalidRefreshToken) {
			t.Fatalf("unexpected concurrent refresh error: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successful concurrent rotations=%d, want=1", successes)
	}
	if err := service.Logout(services.RefreshRequest{RefreshToken: initial.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	assertFamilyAccess(t, service, initial.AccessToken, http.StatusUnauthorized)
}

func waitForFamilyLockWaiters(t *testing.T, db *gorm.DB, count int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var waiting int64
		if err := db.Raw("SELECT COUNT(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'").Scan(&waiting).Error; err != nil {
			t.Fatal(err)
		}
		if waiting >= count {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not observe %d queued session writers", count)
}

func TestSessionFamilyLogoutRotationLockOrders(t *testing.T) {
	db := resetIntegrationDB(t)
	for _, rotationFirst := range []bool{true, false} {
		name := "logout-first"
		if rotationFirst {
			name = "rotation-first"
		}
		t.Run(name, func(t *testing.T) {
			user := resetTestUser(t, db)
			repo := repository.NewRefreshTokenRepository(db)
			current := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
			if err := repo.Create(current); err != nil {
				t.Fatal(err)
			}
			replacement := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
			holder := db.Begin()
			defer holder.Rollback()
			if err := holder.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", user.ID).Error; err != nil {
				t.Fatal(err)
			}
			rotationDone, logoutDone := make(chan error, 1), make(chan error, 1)
			rotate := func() { rotationDone <- repo.Rotate(current.ID, replacement, time.Now()) }
			logout := func() { logoutDone <- repo.RevokeByHash(current.TokenHash, time.Now()) }
			if rotationFirst {
				go rotate()
			} else {
				go logout()
			}
			waitForFamilyLockWaiters(t, db, 1)
			if rotationFirst {
				go logout()
			} else {
				go rotate()
			}
			waitForFamilyLockWaiters(t, db, 2)
			if err := holder.Commit().Error; err != nil {
				t.Fatal(err)
			}
			if err := <-logoutDone; err != nil {
				t.Fatal(err)
			}
			err := <-rotationDone
			if (rotationFirst && err != nil) || (!rotationFirst && !errors.Is(err, repository.ErrNotFound)) {
				t.Fatalf("rotation outcome for %s: %v", name, err)
			}
			active, err := repo.HasActiveFamily(context.Background(), user.ID, 0, current.ID, time.Now())
			if err != nil || active {
				t.Fatalf("family remained active after %s: active=%v error=%v", name, active, err)
			}
		})
	}
}

func TestSessionFamilyCannotChangeDuringRotation(t *testing.T) {
	db := resetIntegrationDB(t)
	user := resetTestUser(t, db)
	repo := repository.NewRefreshTokenRepository(db)
	current := &models.RefreshToken{UserID: user.ID, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.Create(current); err != nil {
		t.Fatal(err)
	}
	wrongFamily := uuid.New()
	replacement := &models.RefreshToken{UserID: user.ID, FamilyID: &wrongFamily, TokenHash: resetTestHash(), ExpiresAt: time.Now().Add(time.Hour)}
	if err := repo.Rotate(current.ID, replacement, time.Now()); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("rotation changed family: %v", err)
	}
	if _, err := repo.FindValid(current.TokenHash, time.Now()); err != nil {
		t.Fatalf("rejected family change consumed current token: %v", err)
	}
}
