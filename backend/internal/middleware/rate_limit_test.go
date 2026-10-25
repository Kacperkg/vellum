package middleware

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestAuthRateLimiterLimitsEveryAuthEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []AuthEndpoint{LoginEndpoint, RegisterEndpoint, RefreshEndpoint, LogoutEndpoint} {
		t.Run(string(endpoint), func(t *testing.T) {
			limiter := testLimiter(endpoint, 2, time.Minute)
			router := gin.New()
			router.POST("/"+string(endpoint), limiter.Handler(endpoint), func(c *gin.Context) {
				c.JSON(http.StatusOK, gin.H{"ok": true})
			})

			body := `{"email":"person@example.test","refresh_token":"opaque-token"}`
			for i, want := range []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests} {
				response := performRateLimitRequest(router, "/"+string(endpoint), body, "192.0.2.10:1234", "")
				if response.Code != want {
					t.Fatalf("request %d status = %d, want %d", i+1, response.Code, want)
				}
				if want == http.StatusTooManyRequests {
					if response.Header().Get("Retry-After") != "60" {
						t.Fatalf("Retry-After = %q, want 60", response.Header().Get("Retry-After"))
					}
					var responseBody map[string]string
					if err := json.Unmarshal(response.Body.Bytes(), &responseBody); err != nil {
						t.Fatal(err)
					}
					if responseBody["error"] != rateLimitError {
						t.Fatalf("error = %q, want %q", responseBody["error"], rateLimitError)
					}
				}
			}
		})
	}
}

func TestAuthRateLimiterExpiresWindows(t *testing.T) {
	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	limiter := testLimiter(LoginEndpoint, 1, time.Minute)
	limiter.now = func() time.Time { return now }
	router := gin.New()
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	body := `{"email":"person@example.test"}`
	if got := performRateLimitRequest(router, "/login", body, "192.0.2.10:1234", "").Code; got != http.StatusNoContent {
		t.Fatalf("first status = %d", got)
	}
	if got := performRateLimitRequest(router, "/login", body, "192.0.2.10:1234", "").Code; got != http.StatusTooManyRequests {
		t.Fatalf("limited status = %d", got)
	}
	now = now.Add(time.Minute)
	if got := performRateLimitRequest(router, "/login", body, "192.0.2.10:1234", "").Code; got != http.StatusNoContent {
		t.Fatalf("status after expiry = %d", got)
	}
}

func TestAuthRateLimiterConcurrentRequests(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 10, time.Minute)
	// Isolate quota atomicity from the separately tested concurrency guard.
	limiter.work.maxTotal = 100
	limiter.work.maxNetwork = 100
	router := gin.New()
	var handled atomic.Int32
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) {
		handled.Add(1)
		c.Status(http.StatusNoContent)
	})

	const requests = 100
	var allowed, rejected atomic.Int32
	var wg sync.WaitGroup
	wg.Add(requests)
	for i := 0; i < requests; i++ {
		go func() {
			defer wg.Done()
			response := performRateLimitRequest(router, "/login", `{"email":"same@example.test"}`, "192.0.2.10:1234", "")
			switch response.Code {
			case http.StatusNoContent:
				allowed.Add(1)
			case http.StatusTooManyRequests:
				rejected.Add(1)
			default:
				t.Errorf("unexpected status %d", response.Code)
			}
		}()
	}
	wg.Wait()
	if allowed.Load() != 10 || rejected.Load() != 90 || handled.Load() != 10 {
		t.Fatalf("allowed=%d rejected=%d handled=%d", allowed.Load(), rejected.Load(), handled.Load())
	}
}

func TestAuthRateLimiterBoundsConcurrentWorkAndReleasesSlots(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 100, time.Minute)
	router := gin.New()
	entered := make(chan struct{}, limiter.work.maxTotal)
	release := make(chan struct{})
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) {
		entered <- struct{}{}
		<-release
		c.Status(http.StatusNoContent)
	})
	var wg sync.WaitGroup
	for i := 0; i < limiter.work.maxTotal; i++ {
		wg.Add(1)
		go func(client int) {
			defer wg.Done()
			performRateLimitRequest(router, "/login", `{}`, fmt.Sprintf("192.0.2.%d:1234", client+1), "")
		}(i)
	}
	for i := 0; i < limiter.work.maxTotal; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			wg.Wait()
			t.Fatal("auth workers did not enter")
		}
	}
	response := performRateLimitRequest(router, "/login", `{}`, "192.0.2.2:1234", "")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "1" {
		t.Errorf("concurrency status=%d retry=%s", response.Code, response.Header().Get("Retry-After"))
	}
	close(release)
	wg.Wait()
	if limiter.work.total != 0 || len(limiter.work.networks) != 0 {
		t.Fatal("auth slots leaked")
	}
	if got := performRateLimitRequest(router, "/login", `{}`, "192.0.2.2:1234", "").Code; got != http.StatusNoContent {
		t.Fatalf("status after work completes=%d", got)
	}
}

func TestAuthRateLimiterBoundsBodiesAndChargesBeforeReading(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 1, time.Minute)
	limiter.sharedIPMultiplier = 1
	router := gin.New()
	var handled int
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) {
		handled++
		c.Status(http.StatusNoContent)
	})
	oversized := strings.Repeat("x", maxAuthBodyBytes+1)
	response := performRateLimitRequest(router, "/login", oversized, "192.0.2.1:1", "")
	if response.Code != http.StatusRequestEntityTooLarge || handled != 0 {
		t.Fatalf("oversized status=%d handled=%d", response.Code, handled)
	}
	request := httptest.NewRequest(http.MethodPost, "/login", nil)
	request.RemoteAddr = "192.0.2.1:1"
	request.Body = forbiddenBody{t}
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("rejected before read status=%d", response.Code)
	}
}

func TestSingleNetworkCannotMonopolizeAuthWork(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 100, time.Minute)
	router := gin.New()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) {
		if c.Request.RemoteAddr == "192.0.2.1:1" {
			entered <- struct{}{}
			<-release
		}
		c.Status(http.StatusNoContent)
	})
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			performRateLimitRequest(router, "/login", `{}`, "192.0.2.1:1", "")
		}()
	}
	defer func() { close(release); wg.Wait() }()
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("network workers did not enter")
		}
	}
	if got := performRateLimitRequest(router, "/login", `{}`, "192.0.2.1:1", "").Code; got != 429 {
		t.Fatalf("single-network cap status=%d", got)
	}
	if got := performRateLimitRequest(router, "/login", `{}`, "192.0.2.2:1", "").Code; got != 204 {
		t.Fatalf("other network starved: status=%d", got)
	}
}

type forbiddenBody struct{ t *testing.T }

func (b forbiddenBody) Read([]byte) (int, error) {
	b.t.Error("limited request body was read")
	return 0, io.EOF
}
func (b forbiddenBody) Close() error { return nil }

func TestAuthRateLimiterRetryAfterRoundingAndSlowBody(t *testing.T) {
	now := time.Date(2026, time.October, 25, 12, 0, 0, 0, time.UTC)
	limiter := testLimiter(LoginEndpoint, 1, time.Minute)
	limiter.now = func() time.Time { return now }
	router := gin.New()
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodPost, "/login", nil)
	request.RemoteAddr = "192.0.2.1:1"
	request.Body = &delayedBody{Reader: strings.NewReader(`{"email":"a@example.test"}`), advance: func() { now = now.Add(30 * time.Second) }}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	now = now.Add(500 * time.Millisecond)
	response = performRateLimitRequest(router, "/login", `{"email":"a@example.test"}`, "192.0.2.1:1", "")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "60" {
		t.Fatalf("slow-body retry status=%d retry=%s", response.Code, response.Header().Get("Retry-After"))
	}
	now = now.Add(59 * time.Second)
	response = performRateLimitRequest(router, "/login", `{"email":"a@example.test"}`, "192.0.2.1:1", "")
	if response.Header().Get("Retry-After") != "1" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("fractional retry headers=%v", response.Header())
	}
}

type delayedBody struct {
	*strings.Reader
	advance func()
}

func (b *delayedBody) Read(p []byte) (int, error) {
	if b.advance != nil {
		b.advance()
		b.advance = nil
	}
	return b.Reader.Read(p)
}
func (b *delayedBody) Close() error { return nil }

func TestStoreSaturationPreservesActiveLimitsAndExpiresInactiveKeys(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 1, time.Minute)
	store := limiter.store
	now := time.Now()
	limit := RateLimit{Requests: 1, Window: time.Minute}
	if allowed, _ := store.allow("victim", limit, now); !allowed {
		t.Fatal("initial victim denied")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				store.allow(fmt.Sprintf("client-%d-%d", worker, j), limit, now)
			}
		}(i)
	}
	wg.Wait()
	if allowed, retry := store.allow("victim", limit, now); allowed || retry != time.Minute {
		t.Fatalf("active victim quota lost: allowed=%v retry=%v", allowed, retry)
	}
	total := 0
	for i := range store.shards {
		shard := &store.shards[i]
		if len(shard.entries) > shard.max || len(shard.expiry) != len(shard.entries) {
			t.Fatal("store or heap grew beyond capacity")
		}
		total += len(shard.entries)
	}
	if total != 1024 {
		t.Fatalf("saturated entries=%d", total)
	}
	for i := 0; i < len(store.shards); i++ {
		store.cleanup(now.Add(time.Minute))
	}
	for i := range store.shards {
		if len(store.shards[i].entries) != 0 || len(store.shards[i].expiry) != 0 {
			t.Fatal("inactive expired entries retained after sweep")
		}
	}
}

func TestStoreCapacityRetryUsesEarliestExpiry(t *testing.T) {
	store := &boundedStore{shards: []rateShard{{entries: make(map[[32]byte]*rateEntry), max: 2}}}
	now := time.Now()
	store.allow("long", RateLimit{Requests: 1, Window: time.Hour}, now)
	store.allow("short", RateLimit{Requests: 1, Window: time.Minute}, now)
	if allowed, retry := store.allow("new", RateLimit{Requests: 1, Window: time.Minute}, now); allowed || retry != time.Minute {
		t.Fatalf("capacity allowed=%v retry=%v", allowed, retry)
	}
	if allowed, _ := store.allow("new", RateLimit{Requests: 1, Window: time.Minute}, now.Add(time.Minute)); !allowed {
		t.Fatal("expired capacity not reclaimed")
	}
	key := sha256.Sum256([]byte("long"))
	if _, ok := store.shards[0].entries[key]; !ok {
		t.Fatal("unexpired quota evicted")
	}
}

func TestAuthRateLimiterCopiesConfiguration(t *testing.T) {
	limits := map[AuthEndpoint]RateLimit{LoginEndpoint: {Requests: 1, Window: time.Minute}}
	limiter := NewAuthRateLimiter(limits, 100, 20)
	limits[LoginEndpoint] = RateLimit{Requests: 100, Window: time.Hour}
	if limiter.limits[LoginEndpoint].Requests != 1 {
		t.Fatal("caller mutated shared limiter config")
	}
}

func TestAuthRateLimiterDoesNotTrustSpoofedForwardedFor(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 1, time.Minute)
	router := gin.New()
	if err := router.SetTrustedProxies([]string{"10.0.0.2/32"}); err != nil {
		t.Fatal(err)
	}
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	first := performRateLimitRequest(router, "/login", `{}`, "198.51.100.20:1234", "203.0.113.1")
	second := performRateLimitRequest(router, "/login", `{}`, "198.51.100.20:1234", "203.0.113.2")
	if first.Code != http.StatusNoContent || second.Code != http.StatusTooManyRequests {
		t.Fatalf("untrusted proxy statuses = %d, %d", first.Code, second.Code)
	}

	fromClientOne := performRateLimitRequest(router, "/login", `{}`, "10.0.0.2:1234", "192.0.2.1")
	fromClientTwo := performRateLimitRequest(router, "/login", `{}`, "10.0.0.2:1234", "192.0.2.2")
	if fromClientOne.Code != http.StatusNoContent || fromClientTwo.Code != http.StatusNoContent {
		t.Fatalf("trusted proxy statuses = %d, %d", fromClientOne.Code, fromClientTwo.Code)
	}

	spoofedChain := performRateLimitRequest(router, "/login", `{}`, "10.0.0.2:1234", "203.0.113.99, 192.0.2.2")
	if spoofedChain.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed chain status = %d, want 429", spoofedChain.Code)
	}
}

func TestAuthRateLimiterSeparatesIdentitiesBehindSharedIP(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 1, time.Minute)
	router := gin.New()
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, email := range []string{"one@example.test", "two@example.test"} {
		response := performRateLimitRequest(router, "/login", `{"email":"`+email+`"}`, "192.0.2.10:1234", "")
		if response.Code != http.StatusNoContent {
			t.Fatalf("%s status = %d", email, response.Code)
		}
	}
}

func TestAuthRateLimiterPreservesRequestBody(t *testing.T) {
	limiter := testLimiter(LoginEndpoint, 1, time.Minute)
	router := gin.New()
	router.POST("/login", limiter.Handler(LoginEndpoint), func(c *gin.Context) {
		var request struct {
			Email    string `json:"email" binding:"required"`
			Password string `json:"password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&request); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, request)
	})

	body := `{"email":"person@example.test","password":"unchanged"}`
	response := performRateLimitRequest(router, "/login", body, "192.0.2.10:1234", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"password":"unchanged"`) {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestBoundedStoreNeverExceedsConfiguredCapacity(t *testing.T) {
	limiter := NewAuthRateLimiter(map[AuthEndpoint]RateLimit{
		LoginEndpoint: {Requests: 1, Window: time.Hour},
	}, 64, 20)
	now := time.Now()
	for i := 0; i < 1000; i++ {
		limiter.store.allow(string(rune(i)), RateLimit{Requests: 1, Window: time.Hour}, now)
	}

	total := 0
	for i := range limiter.store.shards {
		limiter.store.shards[i].Lock()
		total += len(limiter.store.shards[i].entries)
		limiter.store.shards[i].Unlock()
	}
	if total > 64 {
		t.Fatalf("stored entries = %d, want at most 64", total)
	}
}

func testLimiter(endpoint AuthEndpoint, requests int, window time.Duration) *AuthRateLimiter {
	return NewAuthRateLimiter(map[AuthEndpoint]RateLimit{
		endpoint: {Requests: requests, Window: window},
	}, 1024, 20)
}

func performRateLimitRequest(router http.Handler, path, body, remoteAddr, forwardedFor string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		request.Header.Set("X-Forwarded-For", forwardedFor)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}
