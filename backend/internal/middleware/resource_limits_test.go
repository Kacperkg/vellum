package middleware

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func resourceRouter(l *ResourceLimiter, handler gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(l.SourceHandler(), func(c *gin.Context) {
		id, _ := uuid.Parse(c.GetHeader("X-Test-User"))
		c.Set("userID", id)
		c.Next()
	}, l.UserHandler())
	r.Any("/resource", handler)
	return r
}

func resourceRequest(r http.Handler, id uuid.UUID, source, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/resource", strings.NewReader(body))
	req.RemoteAddr = source
	req.Header.Set("X-Test-User", id.String())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func resourceBoundaryQuery() string {
	var fields []string
	for i := 0; i < 8; i++ {
		fields = append(fields, fmt.Sprintf("k%d=%s", i, strings.Repeat("x", 252)))
	}
	return strings.Join(fields, "&") + "x"
}

func TestResourceBodyAndQueryBounds(t *testing.T) {
	for _, test := range []struct {
		name, body, query string
		length            int64
		want              int
	}{
		{"boundary", strings.Repeat(" ", MaxResourceBodyBytes), "", MaxResourceBodyBytes, 204},
		{"declared oversize", "", "", MaxResourceBodyBytes + 1, 413},
		{"chunked oversize", strings.Repeat("x", MaxResourceBodyBytes+1), "", -1, 413},
		{"trailing oversize", `{}` + strings.Repeat(" ", MaxResourceBodyBytes), "", -1, 413},
		{"lying length", strings.Repeat("x", MaxResourceBodyBytes+1), "", 2, 413},
		{"query boundary", "", resourceBoundaryQuery(), 0, 204},
		{"duplicate filter", "", "frequency=daily&frequency=yearly", 0, 400},
		{"malformed query", "", "frequency=%ZZ", 0, 400},
		{"filter oversize", "", "frequency=" + strings.Repeat("x", 257), 0, 400},
		{"query oversize", "", strings.Repeat("x", MaxResourceQueryBytes+1), 0, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			called := false
			r := resourceRouter(NewResourceLimiter(), func(c *gin.Context) { called = true; c.Status(204) })
			req := httptest.NewRequest("POST", "/resource?"+test.query, strings.NewReader(test.body))
			req.ContentLength = test.length
			req.Header.Set("X-Test-User", uuid.NewString())
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != test.want || called != (test.want == 204) {
				t.Fatalf("status=%d handler=%v", w.Code, called)
			}
		})
	}
}

func TestResourceBodyFailureAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		l := NewResourceLimiter()
		r := resourceRouter(l, func(c *gin.Context) { t.Error("failed body reached handler") })
		ctx, cancel := context.WithCancel(context.Background())
		req := httptest.NewRequest("POST", "/resource", nil).WithContext(ctx)
		req.Header.Set("X-Test-User", uuid.NewString())
		req.Body = &resourceFailedBody{cancel: func() {
			if canceled {
				cancel()
			}
		}}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		cancel()
		want := 400
		if canceled {
			want = 408
		}
		if w.Code != want {
			t.Fatalf("status=%d want=%d", w.Code, want)
		}
		if l.sources.total != 0 || l.users.total != 0 {
			t.Fatal("failed body leaked slots")
		}
	}
}

type resourceFailedBody struct{ cancel func() }

func (b *resourceFailedBody) Read([]byte) (int, error) { b.cancel(); return 0, io.ErrUnexpectedEOF }
func (b *resourceFailedBody) Close() error             { return nil }

func TestResourceDeadlineAndParentCancellation(t *testing.T) {
	for _, deadline := range []bool{true, false} {
		l := NewResourceLimiter()
		l.timeout = 5 * time.Millisecond
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		r := resourceRouter(l, func(c *gin.Context) {
			if !deadline {
				cancel()
			}
			<-c.Request.Context().Done()
			resourceContextActive(c)
		})
		req := httptest.NewRequest("POST", "/resource", nil).WithContext(ctx)
		req.Header.Set("X-Test-User", uuid.NewString())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		want := 504
		if !deadline {
			want = 408
		}
		if w.Code != want {
			t.Fatalf("status=%d want=%d", w.Code, want)
		}
		if l.sources.total != 0 || l.users.total != 0 || len(l.sources.networks) != 0 || len(l.users.networks) != 0 {
			t.Fatal("canceled request leaked slots")
		}
	}
}

func TestResourceConcurrencyLimits(t *testing.T) {
	for _, kind := range []string{"user", "source", "global"} {
		t.Run(kind, func(t *testing.T) {
			l := NewResourceLimiter()
			l.sources.maxTotal = 4
			l.sources.maxNetwork = 3
			l.users.maxNetwork = 2
			entered := make(chan struct{}, 4)
			release := make(chan struct{})
			r := resourceRouter(l, func(c *gin.Context) { entered <- struct{}{}; <-release; c.Status(204) })
			id := uuid.New()
			var wg sync.WaitGroup
			start := func(user uuid.UUID, source string) {
				wg.Add(1)
				go func() { defer wg.Done(); resourceRequest(r, user, source, "") }()
				select {
				case <-entered:
				case <-time.After(time.Second):
					t.Fatal("handler did not enter")
				}
			}
			defer func() { close(release); wg.Wait() }()
			switch kind {
			case "user":
				start(id, "192.0.2.1:1")
				start(id, "192.0.2.2:1")
			case "source":
				for i := 0; i < 3; i++ {
					start(uuid.New(), "192.0.2.1:1")
				}
			case "global":
				for i := 0; i < 4; i++ {
					start(uuid.New(), fmt.Sprintf("192.0.2.%d:1", i+2))
				}
			}
			w := resourceRequest(r, id, "192.0.2.1:1", "")
			if w.Code != 429 || w.Header().Get("Retry-After") != "1" || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("overflow=%d headers=%v", w.Code, w.Header())
			}
		})
	}
}

func TestResourceRateKeysCapacityAndExpiry(t *testing.T) {
	l := NewResourceLimiter()
	l.store = newBoundedStore(1)
	l.userRate.Requests = 1
	// With a one-entry shard, source state consumes capacity. Unknown keys fail
	// closed instead of evicting an existing source's quota.
	now := time.Now()
	l.now = func() time.Time { return now }
	r := resourceRouter(l, func(c *gin.Context) { c.Status(204) })
	id := uuid.New()
	w := resourceRequest(r, id, "192.0.2.1:1", "")
	if w.Code != 429 {
		t.Fatalf("saturated store status=%d", w.Code)
	}
	if len(l.store.shards[0].entries) != 1 || len(l.store.shards[0].expiry) != 1 {
		t.Fatal("state capacity exceeded")
	}
	now = now.Add(time.Minute)
	l.store.cleanup(now)
	if len(l.store.shards[0].entries) != 0 {
		t.Fatal("inactive keys retained")
	}

	l = NewResourceLimiter()
	l.userRate.Requests = 1
	r = resourceRouter(l, func(c *gin.Context) { c.Status(204) })
	if resourceRequest(r, id, "192.0.2.1:1", "").Code != 204 {
		t.Fatal("initial request denied")
	}
	if w = resourceRequest(r, id, "192.0.2.2:1", ""); w.Code != 429 {
		t.Fatal("changing source reset user quota")
	}
	if resourceRequest(r, uuid.New(), "192.0.2.2:1", "").Code != 204 {
		t.Fatal("other user denied")
	}
}

func TestResourceSlotsReleaseOnPanicAndSuccess(t *testing.T) {
	l := NewResourceLimiter()
	r := resourceRouter(l, func(c *gin.Context) { panic("test") })
	func() { defer func() { _ = recover() }(); resourceRequest(r, uuid.New(), "192.0.2.1:1", "") }()
	if l.sources.total != 0 || l.users.total != 0 || len(l.sources.networks) != 0 || len(l.users.networks) != 0 {
		t.Fatal("panic leaked slots")
	}
	r = resourceRouter(l, func(c *gin.Context) { c.Status(204) })
	if resourceRequest(r, uuid.New(), "192.0.2.1:1", "").Code != 204 {
		t.Fatal("slots not reusable")
	}
}

func TestResourceSourceQuotaChargesBeforeAuthenticationAndBody(t *testing.T) {
	l := NewResourceLimiter()
	l.sourceRate.Requests = 1
	r := resourceRouter(l, func(c *gin.Context) { c.Status(204) })
	if resourceRequest(r, uuid.New(), "192.0.2.1:1", "").Code != 204 {
		t.Fatal("first request failed")
	}
	req := httptest.NewRequest("POST", "/resource", nil)
	req.RemoteAddr = "192.0.2.1:1"
	req.Body = forbiddenBody{t}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 429 {
		t.Fatalf("changing user bypassed source quota: %d", w.Code)
	}
}

func BenchmarkResourceAdmission(b *testing.B) {
	gin.SetMode(gin.TestMode)
	for _, limited := range []bool{false, true} {
		b.Run(fmt.Sprintf("limited=%v", limited), func(b *testing.B) {
			var r *gin.Engine
			if limited {
				l := NewResourceLimiter()
				l.sourceRate.Requests = int(^uint(0) >> 1)
				l.userRate.Requests = int(^uint(0) >> 1)
				r = resourceRouter(l, func(c *gin.Context) { c.Status(204) })
			} else {
				r = gin.New()
				r.POST("/resource", func(c *gin.Context) { c.Status(204) })
			}
			id := uuid.New()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				resourceRequest(r, id, "192.0.2.1:1", "")
			}
		})
	}
}
