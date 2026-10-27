package routes_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/config"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/routes"
)

func TestResetRedemptionIdentityAndNetworkLimits(t *testing.T) {
	cfg := securityConfig()
	cfg.ResetPasswordRateLimit = config.RateLimit{Requests: 2, Window: time.Minute}
	cfg.RateLimitSharedIPMultiplier = 2
	set := testHandlerSet(uuid.New())
	set.PasswordReset = handlers.NewPasswordResetHandler(nil)
	router, err := routes.NewRouter(cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	request := func(token, peer string, want int) {
		t.Helper()
		body := fmt.Sprintf(`{"token":%q,"new_password":"new-password"}`, token)
		response := chainRequest(router, http.MethodPost, "/reset-password", body, peer, nil)
		if response.Code != want {
			t.Fatalf("redemption status=%d want=%d", response.Code, want)
		}
		if want == http.StatusTooManyRequests && (response.Header().Get("Retry-After") == "" || response.Header().Get("Cache-Control") != "no-store") {
			t.Fatal("redemption rate limit omitted backoff/privacy headers")
		}
	}
	first := strings.Repeat("A", 43)
	second := strings.Repeat("B", 43)
	third := strings.Repeat("C", 43)
	// Invalid redemptions still consume the token/network budget, before hashing.
	request(first, "192.0.2.80:1000", http.StatusBadRequest)
	request(first, "192.0.2.80:1001", http.StatusBadRequest)
	request(first, "192.0.2.80:1002", http.StatusTooManyRequests)
	request(second, "192.0.2.80:1003", http.StatusBadRequest)
	// Changing tokens cannot reset the shared network's four-attempt allowance.
	request(third, "192.0.2.80:1004", http.StatusTooManyRequests)
	// An attacker elsewhere cannot exhaust this token's allowance for all peers.
	request(first, "192.0.2.81:1000", http.StatusBadRequest)
}

func TestRecoveryBodiesAreBoundedBeforeHandlers(t *testing.T) {
	for _, path := range []string{"/forgot-password", "/reset-password"} {
		t.Run(path, func(t *testing.T) {
			set := testHandlerSet(uuid.New())
			set.PasswordReset = handlers.NewPasswordResetHandler(nil)
			router, err := routes.NewRouter(securityConfig(), set)
			if err != nil {
				t.Fatal(err)
			}
			response := chainRequest(router, http.MethodPost, path, strings.Repeat("x", 64<<10+1), "192.0.2.82:1000", nil)
			if response.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized recovery body status=%d", response.Code)
			}
		})
	}
}
