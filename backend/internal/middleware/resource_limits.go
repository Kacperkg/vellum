package middleware

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	MaxResourceBodyBytes   = 64 << 10
	MaxResourceQueryBytes  = 2048
	ResourceRequestTimeout = 10 * time.Second
)

// ResourceLimiter has independent admission state from public authentication.
// There are no work queues or per-request worker goroutines. All slots remain
// held until synchronous handlers finish, including database commit/rollback.
type ResourceLimiter struct {
	store                *boundedStore
	sources              authConcurrency
	users                authConcurrency
	now                  func() time.Time
	timeout              time.Duration
	sourceRate, userRate RateLimit
}

func NewResourceLimiter() *ResourceLimiter {
	return &ResourceLimiter{
		store:   newBoundedStore(10000),
		sources: authConcurrency{networks: make(map[string]int), maxTotal: 32, maxNetwork: 24},
		users:   authConcurrency{networks: make(map[string]int), maxTotal: 32, maxNetwork: 16},
		now:     time.Now, timeout: ResourceRequestTimeout,
		sourceRate: RateLimit{Requests: 2400, Window: time.Minute},
		userRate:   RateLimit{Requests: 240, Window: time.Minute},
	}
}

// SourceHandler runs before authentication's database-backed version check.
func (l *ResourceLimiter) SourceHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		now := l.now()
		l.store.cleanup(now)
		network := clientNetwork(c)
		if allowed, retry := l.store.allow("resource:source:"+network, l.sourceRate, now); !allowed {
			respondRateLimited(c, retry)
			return
		}
		if !l.sources.acquire(network) {
			respondRateLimited(c, time.Second)
			return
		}
		defer l.sources.release(network)
		ctx, cancel := context.WithTimeout(c.Request.Context(), l.timeout)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		if !resourceContextActive(c) {
			return
		}
		c.Next()
	}
}

// UserHandler runs after authentication; keys survive token rotation and source
// changes. Read the entire bounded body before binding or financial side effects.
func (l *ResourceLimiter) UserHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		user, ok := c.Get("userID")
		id, valid := user.(uuid.UUID)
		if !ok || !valid || id == uuid.Nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		key := id.String()
		if allowed, retry := l.store.allow("resource:user:"+key, l.userRate, l.now()); !allowed {
			respondRateLimited(c, retry)
			return
		}
		if !l.users.acquire(key) {
			respondRateLimited(c, time.Second)
			return
		}
		defer l.users.release(key)
		if !resourceContextActive(c) {
			return
		}
		if len(c.Request.URL.RawQuery) > MaxResourceQueryBytes {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
			return
		}
		query, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil || len(query) > 16 {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
			return
		}
		for key, values := range query {
			if len(key) > 64 || len(values) != 1 || len(values[0]) > 256 {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
				return
			}
		}
		if c.Request.ContentLength > MaxResourceBodyBytes {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		if c.Request.Body != nil {
			body, err := io.ReadAll(io.LimitReader(c.Request.Body, MaxResourceBodyBytes+1))
			if len(body) > MaxResourceBodyBytes {
				c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			if !resourceContextActive(c) {
				return
			}
			if err != nil {
				c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
				return
			}
			c.Request.Body = io.NopCloser(bytes.NewReader(body))
		}
		c.Next()
	}
}

func resourceContextActive(c *gin.Context) bool {
	switch c.Request.Context().Err() {
	case context.DeadlineExceeded:
		c.AbortWithStatusJSON(http.StatusGatewayTimeout, gin.H{"error": "request timed out"})
		return false
	case context.Canceled:
		c.AbortWithStatusJSON(http.StatusRequestTimeout, gin.H{"error": "request canceled"})
		return false
	default:
		return true
	}
}
