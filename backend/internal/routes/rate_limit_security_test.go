package routes_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/config"
	"github.com/kacperkg/vellum/internal/handlers"
	"github.com/kacperkg/vellum/internal/routes"
	"github.com/kacperkg/vellum/internal/services"
)

func securityConfig() *config.Config {
	return &config.Config{
		JWTSecret:                   "test-secret",
		CORSAllowedOrigins:          []string{"http://localhost:5173"},
		LoginRateLimit:              config.RateLimit{Requests: 2, Window: time.Minute},
		RegisterRateLimit:           config.RateLimit{Requests: 2, Window: time.Minute},
		RefreshRateLimit:            config.RateLimit{Requests: 2, Window: time.Minute},
		LogoutRateLimit:             config.RateLimit{Requests: 2, Window: time.Minute},
		RateLimitMaxEntries:         10000,
		RateLimitSharedIPMultiplier: 20,
	}
}

func securityRouter(t *testing.T, cfg *config.Config) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router, err := routes.NewRouter(cfg, testHandlerSet(uuid.New()))
	if err != nil {
		t.Fatal(err)
	}
	return router
}

func chainRequest(router http.Handler, method, path, body, remote string, headers http.Header) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = remote
	request.Header.Set("Content-Type", "application/json")
	for key, values := range headers {
		request.Header[key] = values
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func TestAllAuthEndpointsThroughProductionChain(t *testing.T) {
	for _, path := range []string{"/login", "/register", "/refresh", "/logout"} {
		t.Run(path, func(t *testing.T) {
			router := securityRouter(t, securityConfig())
			for i, want := range []int{400, 400, 429} {
				response := chainRequest(router, http.MethodPost, path, `{}`, "192.0.2.10:1", nil)
				if response.Code != want {
					t.Fatalf("request %d status=%d body=%s", i, response.Code, response.Body)
				}
				if want == 429 {
					if response.Body.String() != `{"error":"rate limit exceeded"}` || response.Header().Get("Retry-After") != "60" {
						t.Fatalf("429 contract: headers=%v body=%s", response.Header(), response.Body)
					}
				}
			}
		})
	}
}

func TestAuthenticationSuccessFailureRotationAndLogout(t *testing.T) {
	cfg := securityConfig()
	cfg.RateLimitSharedIPMultiplier = 2
	users := &fakeUserRepository{}
	tokens := &fakeRefreshTokenRepository{}
	handlers := testHandlerSet(uuid.New())
	handlers.Auth = authHandlerForTest(users, tokens)
	router, err := routes.NewRouter(cfg, handlers)
	if err != nil {
		t.Fatal(err)
	}
	request := func(path, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		response := chainRequest(router, http.MethodPost, path, body, "192.0.2.10:1", nil)
		if response.Code != want {
			t.Fatalf("%s status=%d want=%d body=%s", path, response.Code, want, response.Body)
		}
		return response
	}
	registration := `{"email":"person@example.test","first_name":"Test","last_name":"Person","password":"password123"}`
	request("/register", registration, http.StatusCreated)
	users.User = users.Created
	request("/register", registration, http.StatusConflict)
	request("/register", registration, http.StatusTooManyRequests)

	request("/login", `{"email":"person@example.test","password":"incorrect"}`, http.StatusUnauthorized)
	login := request("/login", `{"email":"person@example.test","password":"password123"}`, http.StatusOK)
	request("/login", `{"email":"person@example.test","password":"password123"}`, http.StatusTooManyRequests)
	tokenFrom := func(response *httptest.ResponseRecorder) string {
		t.Helper()
		var responseBody struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &responseBody); err != nil {
			t.Fatal(err)
		}
		if responseBody.RefreshToken == "" || responseBody.AccessToken == "" {
			t.Fatal("successful auth omitted tokens")
		}
		return responseBody.RefreshToken
	}
	refreshBody := func(token string) string { return fmt.Sprintf(`{"refresh_token":%q}`, token) }
	oldToken := tokenFrom(login)
	firstRotation := tokenFrom(request("/refresh", refreshBody(oldToken), http.StatusOK))
	request("/refresh", refreshBody(oldToken), http.StatusUnauthorized)
	request("/refresh", refreshBody(oldToken), http.StatusTooManyRequests)
	newToken := tokenFrom(request("/refresh", refreshBody(firstRotation), http.StatusOK))
	// A freshly rotated token cannot reset the network bucket (four attempts).
	request("/refresh", refreshBody(newToken), http.StatusTooManyRequests)
	request("/logout", refreshBody(newToken), http.StatusNoContent)
	request("/logout", refreshBody(newToken), http.StatusNoContent)
	request("/logout", refreshBody(newToken), http.StatusTooManyRequests)
	if tokens.Token.RevokedAt == nil {
		t.Fatal("logout failed to revoke token")
	}
}

func authHandlerForTest(users *fakeUserRepository, tokens *fakeRefreshTokenRepository) *handlers.AuthHandler {
	return handlers.NewAuthHandler(services.NewUserService(users, tokens, "test-secret", time.Minute, time.Hour))
}

func TestParserVariantsCannotChangeAcceptedRequestIdentity(t *testing.T) {
	for _, test := range []struct {
		name, path, baseline, variant string
		want                          int
	}{
		{"login irrelevant type", "/login", `{"email":"a@example.test","password":"pass"}`, `{"email":"a@example.test","password":"pass","refresh_token":123}`, 401},
		{"login trailing JSON", "/login", `{"email":"a@example.test","password":"pass"}`, `{"email":"a@example.test","password":"pass"}{"email":"b@example.test"}`, 401},
		{"refresh irrelevant type", "/refresh", `{"refresh_token":"invalid"}`, `{"refresh_token":"invalid","email":123}`, 401},
		{"logout trailing data", "/logout", `{"refresh_token":"invalid"}`, `{"refresh_token":"invalid"}ignored`, 204},
		{"email case", "/login", `{"email":"a@example.test","password":"pass"}`, `{"EMAIL":"A@EXAMPLE.TEST","password":"pass"}`, 401},
		{"duplicate email", "/login", `{"email":"a@example.test","password":"pass"}`, `{"email":"different@example.test","email":"a@example.test","password":"pass"}`, 401},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := securityRouter(t, securityConfig())
			for _, body := range []string{test.baseline, test.variant} {
				response := chainRequest(router, "POST", test.path, body, "192.0.2.10:1", nil)
				if response.Code != test.want {
					t.Fatalf("status=%d want=%d body=%s", response.Code, test.want, response.Body)
				}
			}
			if got := chainRequest(router, "POST", test.path, test.variant, "192.0.2.10:1", nil).Code; got != 429 {
				t.Fatalf("variant bypass status=%d", got)
			}
		})
	}
}

func TestClientIPTrustAndCanonicalization(t *testing.T) {
	for _, test := range []struct {
		name, firstRemote, secondRemote string
		first, second                   http.Header
		separate                        bool
	}{
		{name: "untrusted XFF", firstRemote: "198.51.100.10:1", secondRemote: "198.51.100.10:2", first: http.Header{"X-Forwarded-For": {"192.0.2.1"}}, second: http.Header{"X-Forwarded-For": {"192.0.2.2"}}},
		{name: "untrusted XReal", firstRemote: "198.51.100.10:1", secondRemote: "198.51.100.10:2", first: http.Header{"X-Real-Ip": {"192.0.2.1"}}, second: http.Header{"X-Real-Ip": {"192.0.2.2"}}},
		{name: "trusted distinct clients", firstRemote: "10.0.0.2:1", secondRemote: "10.0.0.2:1", first: http.Header{"X-Forwarded-For": {"192.0.2.1"}}, second: http.Header{"X-Forwarded-For": {"192.0.2.2"}}, separate: true},
		{name: "spoofed prefix", firstRemote: "10.0.0.2:1", secondRemote: "10.0.0.2:1", first: http.Header{"X-Forwarded-For": {"192.0.2.1"}}, second: http.Header{"X-Forwarded-For": {"203.0.113.99, 192.0.2.1"}}},
		{name: "malformed XFF ignores XReal", firstRemote: "10.0.0.2:1", secondRemote: "10.0.0.2:1", first: http.Header{"X-Forwarded-For": {"invalid"}, "X-Real-Ip": {"192.0.2.1"}}, second: http.Header{"X-Forwarded-For": {"bad"}, "X-Real-Ip": {"192.0.2.2"}}},
		{name: "missing XFF ignores XReal", firstRemote: "10.0.0.2:1", secondRemote: "10.0.0.2:1", first: http.Header{"X-Real-Ip": {"192.0.2.1"}}, second: http.Header{"X-Real-Ip": {"192.0.2.2"}}},
		{name: "repeated XFF", firstRemote: "10.0.0.2:1", secondRemote: "10.0.0.2:1", first: http.Header{"X-Forwarded-For": {"192.0.2.1", "192.0.2.2"}}, second: http.Header{"X-Forwarded-For": {"203.0.113.1", "203.0.113.2"}}},
		{name: "IPv4 mapped", firstRemote: "192.0.2.1:1", secondRemote: "[::ffff:192.0.2.1]:2"},
		{name: "IPv6 spelling", firstRemote: "10.0.0.2:1", secondRemote: "10.0.0.2:1", first: http.Header{"X-Forwarded-For": {"2001:db8:abcd::1"}}, second: http.Header{"X-Forwarded-For": {"2001:0DB8:ABCD:0000:0000:0000:0000:0001"}}},
		{name: "IPv6 privacy rotation", firstRemote: "[2001:db8:abcd::1]:1", secondRemote: "[2001:db8:abcd::ffff]:2"},
		{name: "IPv6 separate prefixes", firstRemote: "[2001:db8:abcd::1]:1", secondRemote: "[2001:db8:abce::1]:2", separate: true},
		{name: "missing socket peer", firstRemote: "", secondRemote: "", first: http.Header{"X-Forwarded-For": {"192.0.2.1"}}, second: http.Header{"X-Forwarded-For": {"192.0.2.2"}}},
		{name: "malformed socket peer", firstRemote: "invalid:1", secondRemote: "other-invalid:2"},
		{name: "oversized XFF", firstRemote: "10.0.0.2:1", secondRemote: "10.0.0.2:1", first: http.Header{"X-Forwarded-For": {strings.Repeat("x", 4097)}}, second: http.Header{"X-Forwarded-For": {strings.Repeat("y", 4097)}}},
		{name: "trusted IPv6 peer", firstRemote: "[fd00::2]:1", secondRemote: "[fd00::2]:2", first: http.Header{"X-Forwarded-For": {"192.0.2.1"}}, second: http.Header{"X-Forwarded-For": {"192.0.2.2"}}, separate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := securityConfig()
			cfg.TrustedProxyCIDRs = []string{"10.0.0.2/32", "fd00::2/128"}
			router := securityRouter(t, cfg)
			for i := 0; i < 2; i++ {
				if got := chainRequest(router, "POST", "/login", `{}`, test.firstRemote, test.first).Code; got != 400 {
					t.Fatalf("first requests status=%d", got)
				}
			}
			want := 429
			if test.separate {
				want = 400
			}
			if got := chainRequest(router, "POST", "/login", `{}`, test.secondRemote, test.second).Code; got != want {
				t.Fatalf("second client status=%d want=%d", got, want)
			}
		})
	}
}

func TestSharedIPQuotaAndRemoteVictimCannotBeLockedOut(t *testing.T) {
	cfg := securityConfig()
	cfg.RateLimitSharedIPMultiplier = 2
	router := securityRouter(t, cfg)
	for _, email := range []string{"one", "two", "three", "four", "five"} {
		response := chainRequest(router, "POST", "/login", fmt.Sprintf(`{"email":"%s@example.test","password":"pass"}`, email), "192.0.2.1:1", nil)
		want := 401
		if email == "five" {
			want = 429
		}
		if response.Code != want {
			t.Fatalf("%s status=%d want=%d", email, response.Code, want)
		}
	}
	// Repeating a known victim's identity from another network cannot consume
	// the legitimate network's identity bucket.
	body := `{"email":"victim@example.test","password":"pass"}`
	for i := 0; i < 3; i++ {
		chainRequest(router, "POST", "/login", body, "198.51.100.1:1", nil)
	}
	if got := chainRequest(router, "POST", "/login", body, "203.0.113.1:1", nil).Code; got != 401 {
		t.Fatalf("remote victim locked out: %d", got)
	}
}

func TestCORSHealthProtectedRoutesAndRouteVariants(t *testing.T) {
	router := securityRouter(t, securityConfig())
	headers := http.Header{"Origin": {"http://localhost:5173"}}
	for i := 0; i < 3; i++ {
		response := chainRequest(router, "POST", "/login", `{}`, "192.0.2.1:1", headers)
		if i == 2 && (response.Code != 429 || response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" || !strings.Contains(response.Header().Get("Access-Control-Expose-Headers"), "Retry-After")) {
			t.Fatalf("CORS 429: status=%d headers=%v", response.Code, response.Header())
		}
	}
	headers.Set("Access-Control-Request-Method", "POST")
	headers.Set("Access-Control-Request-Headers", "content-type")
	if got := chainRequest(router, "OPTIONS", "/login", "", "192.0.2.1:1", headers).Code; got != 204 {
		t.Fatalf("preflight status=%d", got)
	}
	if got := chainRequest(router, "GET", "/ping", "", "192.0.2.1:1", nil).Code; got != 200 {
		t.Fatalf("ping status=%d", got)
	}
	if got := chainRequest(router, "GET", "/api/accounts", "", "192.0.2.1:1", nil).Code; got != 401 {
		t.Fatalf("protected status=%d", got)
	}
	for _, path := range []string{"/login?ignored=1", "/%6cogin"} {
		if got := chainRequest(router, "POST", path, `{}`, "192.0.2.1:1", nil).Code; got != 429 {
			t.Fatalf("canonical alias %s bypass: %d", path, got)
		}
	}
	for _, test := range []struct {
		method, path string
		want         int
	}{
		{"GET", "/login", 404}, {"HEAD", "/login", 404}, {"PUT", "/login", 404}, {"POST", "/LOGIN", 404}, {"POST", "//login", 404}, {"POST", "/api/login", 404}, {"POST", "/login/", 307},
	} {
		if got := chainRequest(router, test.method, test.path, `{}`, "192.0.2.1:1", nil).Code; got != test.want {
			t.Fatalf("%s %s status=%d want=%d", test.method, test.path, got, test.want)
		}
	}
}

func TestAuthOptionsOriginAndRoutingContract(t *testing.T) {
	router := securityRouter(t, securityConfig())
	for _, test := range []struct {
		name, origin string
		want         int
	}{
		{"no origin", "", http.StatusNotFound},
		{"same origin", "http://example.com", http.StatusNotFound},
		{"allowed cross origin", "http://localhost:5173", http.StatusNoContent},
		{"untrusted cross origin", "https://untrusted.example.invalid", http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, path := range []string{"/login", "/register", "/refresh", "/logout"} {
				headers := http.Header{
					"Access-Control-Request-Method":  {"POST"},
					"Access-Control-Request-Headers": {"content-type"},
				}
				if test.origin != "" {
					headers.Set("Origin", test.origin)
				}
				response := chainRequest(router, http.MethodOptions, "http://example.com"+path, "", "192.0.2.1:1", headers)
				if response.Code != test.want {
					t.Fatalf("%s OPTIONS status=%d want=%d", path, response.Code, test.want)
				}
				if test.want == http.StatusNotFound && (response.Body.String() != "404 page not found" || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain")) {
					t.Fatalf("%s unexpected Gin OPTIONS body/type", path)
				}
				if test.want == http.StatusNoContent && response.Header().Get("Access-Control-Allow-Origin") != test.origin {
					t.Fatalf("%s missing allowed-origin response", path)
				}
				if test.want == http.StatusForbidden && response.Header().Get("Access-Control-Allow-Origin") != "" {
					t.Fatalf("%s untrusted origin was allowed", path)
				}
			}
		})
	}
}

func TestProxyConfigurationRejectsInvalidOrUniversalTrust(t *testing.T) {
	for _, proxy := range []string{"invalid", "0.0.0.0/0", "::/0", "::ffff:0.0.0.0/96"} {
		t.Run(proxy, func(t *testing.T) {
			cfg := securityConfig()
			cfg.TrustedProxyCIDRs = []string{proxy}
			if _, err := routes.NewRouter(cfg, testHandlerSet(uuid.New())); err == nil {
				t.Fatal("unsafe proxy configuration accepted")
			}
		})
	}
}

func TestDirectDevelopmentTrustsNoProxyHeaders(t *testing.T) {
	router := securityRouter(t, securityConfig())
	for i := 0; i < 3; i++ {
		headers := http.Header{
			"X-Forwarded-For":         {fmt.Sprintf("192.0.2.%d", i+1)},
			"X-Real-Ip":               {fmt.Sprintf("198.51.100.%d", i+1)},
			"X-Appengine-Remote-Addr": {fmt.Sprintf("203.0.113.%d", i+1)},
		}
		want := 400
		if i == 2 {
			want = 429
		}
		if got := chainRequest(router, "POST", "/login", `{}`, "127.0.0.1:1234", headers).Code; got != want {
			t.Fatalf("default proxy trust bypass: status=%d want=%d", got, want)
		}
	}
}

func TestUniqueClientFloodDoesNotResetExistingQuota(t *testing.T) {
	cfg := securityConfig()
	cfg.RateLimitMaxEntries = 128
	router := securityRouter(t, cfg)
	for i := 0; i < 2; i++ {
		if got := chainRequest(router, "POST", "/login", `{}`, "192.0.2.1:1", nil).Code; got != 400 {
			t.Fatalf("initial request=%d", got)
		}
	}
	for i := 0; i < 1000; i++ {
		ip := fmt.Sprintf("198.18.%d.%d:1", i/256, i%256)
		got := chainRequest(router, "POST", "/login", `{}`, ip, nil).Code
		if got != 400 && got != 429 {
			t.Fatalf("unique client status=%d", got)
		}
	}
	if got := chainRequest(router, "POST", "/login", `{}`, "192.0.2.1:1", nil).Code; got != 429 {
		t.Fatalf("victim quota reset after flood: %d", got)
	}
}

func TestRestartCreatesFreshInMemoryState(t *testing.T) {
	first := securityRouter(t, securityConfig())
	for i := 0; i < 3; i++ {
		chainRequest(first, "POST", "/login", `{}`, "192.0.2.1:1", nil)
	}
	if got := chainRequest(first, "POST", "/login", `{}`, "192.0.2.1:1", nil).Code; got != 429 {
		t.Fatalf("existing limiter status=%d", got)
	}
	restarted := securityRouter(t, securityConfig())
	if got := chainRequest(restarted, "POST", "/login", `{}`, "192.0.2.1:1", nil).Code; got != 400 {
		t.Fatalf("restart did not create fresh state: %d", got)
	}
}
