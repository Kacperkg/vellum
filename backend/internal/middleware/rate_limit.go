package middleware

import (
	"bytes"
	"container/heap"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/codec/json"
)

const rateLimitError = "rate limit exceeded"
const maxAuthBodyBytes = 64 << 10

type RateLimit struct {
	Requests int
	Window   time.Duration
}

type AuthEndpoint string

const (
	LoginEndpoint    AuthEndpoint = "login"
	RegisterEndpoint AuthEndpoint = "register"
	RefreshEndpoint  AuthEndpoint = "refresh"
	LogoutEndpoint   AuthEndpoint = "logout"
)

type rateEntry struct {
	key     [32]byte
	count   int
	resetAt time.Time
}

// Deadlines are immutable: one heap node per map entry, including after
// repeated rejections. There are no per-request timers or worker goroutines.
type expiryHeap []*rateEntry

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].resetAt.Before(h[j].resetAt) }
func (h expiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *expiryHeap) Push(v any)        { *h = append(*h, v.(*rateEntry)) }
func (h *expiryHeap) Pop() any {
	old := *h
	n := len(old) - 1
	entry := old[n]
	old[n] = nil
	*h = old[:n]
	return entry
}

type rateShard struct {
	sync.Mutex
	entries map[[32]byte]*rateEntry
	expiry  expiryHeap
	max     int
}

type boundedStore struct {
	shards []rateShard
	sweep  atomic.Uint64
}

type AuthRateLimiter struct {
	store              *boundedStore
	limits             map[AuthEndpoint]RateLimit
	sharedIPMultiplier int
	work               authConcurrency
	now                func() time.Time
}

type authConcurrency struct {
	sync.Mutex
	networks   map[string]int
	total      int
	maxTotal   int
	maxNetwork int
}

func (a *authConcurrency) acquire(network string) bool {
	a.Lock()
	defer a.Unlock()
	if a.total >= a.maxTotal || a.networks[network] >= a.maxNetwork {
		return false
	}
	a.total++
	a.networks[network]++
	return true
}

func (a *authConcurrency) release(network string) {
	a.Lock()
	defer a.Unlock()
	a.total--
	a.networks[network]--
	if a.networks[network] == 0 {
		delete(a.networks, network)
	}
}

func NewAuthRateLimiter(limits map[AuthEndpoint]RateLimit, maxEntries, sharedIPMultiplier int) *AuthRateLimiter {
	if maxEntries < 1 || sharedIPMultiplier < 1 {
		panic("rate limiter capacity and IP multiplier must be positive")
	}
	immutableLimits := make(map[AuthEndpoint]RateLimit, len(limits))
	for endpoint, limit := range limits {
		if limit.Requests <= 0 || limit.Window <= 0 {
			panic("rate limits must have positive requests and duration")
		}
		immutableLimits[endpoint] = limit
	}
	shardCount := min(32, maxEntries)
	shards := make([]rateShard, shardCount)
	for i := range shards {
		shards[i] = rateShard{entries: make(map[[32]byte]*rateEntry), max: maxEntries / shardCount}
		if i < maxEntries%shardCount {
			shards[i].max++
		}
	}
	return &AuthRateLimiter{
		store:              &boundedStore{shards: shards},
		limits:             immutableLimits,
		sharedIPMultiplier: sharedIPMultiplier,
		work:               authConcurrency{networks: make(map[string]int), maxTotal: 8, maxNetwork: 2},
		now:                time.Now,
	}
}

func (l *AuthRateLimiter) Handler(endpoint AuthEndpoint) gin.HandlerFunc {
	limit, ok := l.limits[endpoint]
	if !ok {
		panic(fmt.Sprintf("missing auth rate limit for %s", endpoint))
	}
	return func(c *gin.Context) {
		now := l.now()
		l.store.cleanup(now)
		network := clientNetwork(c)

		// Charge every attempt before reading its body or allocating identity state.
		// Token rotation and changing emails cannot reset this network safeguard.
		ipLimit := limit
		maxInt := int(^uint(0) >> 1)
		if ipLimit.Requests > maxInt/l.sharedIPMultiplier {
			ipLimit.Requests = maxInt
		} else {
			ipLimit.Requests *= l.sharedIPMultiplier
		}
		if allowed, retry := l.store.allow(string(endpoint)+":network:"+network, ipLimit, now); !allowed {
			respondRateLimited(c, retry)
			return
		}

		// Bound concurrent body buffers and expensive auth work without queuing.
		// Concurrency rejection uses an advisory one-second backoff.
		if !l.work.acquire(network) {
			respondRateLimited(c, time.Second)
			return
		}
		defer l.work.release(network)

		body, ok := readAuthBody(c)
		if !ok {
			return
		}
		identity := requestIdentity(body, endpoint)
		// Scope identity limits to the network so an attacker elsewhere cannot
		// lock out a known email globally. Empty identities share one key.
		if allowed, retry := l.store.allow(string(endpoint)+":identity:"+network+":"+identity, limit, l.now()); !allowed {
			respondRateLimited(c, retry)
			return
		}
		c.Next()
	}
}

func (s *boundedStore) allow(key string, limit RateLimit, now time.Time) (bool, time.Duration) {
	hash := sha256.Sum256([]byte(key))
	shard := &s.shards[int(hash[0])%len(s.shards)]
	shard.Lock()
	defer shard.Unlock()
	shard.expire(now)

	if entry, exists := shard.entries[hash]; exists {
		if entry.count >= limit.Requests {
			return false, entry.resetAt.Sub(now)
		}
		entry.count++
		return true, 0
	}
	if len(shard.entries) >= shard.max {
		// Never discard active counters: unique-key floods must not reset quotas.
		return false, shard.expiry[0].resetAt.Sub(now)
	}
	entry := &rateEntry{key: hash, count: 1, resetAt: now.Add(limit.Window)}
	shard.entries[hash] = entry
	heap.Push(&shard.expiry, entry)
	return true, 0
}

func (s *rateShard) expire(now time.Time) {
	for len(s.expiry) > 0 && !now.Before(s.expiry[0].resetAt) {
		entry := heap.Pop(&s.expiry).(*rateEntry)
		delete(s.entries, entry.key)
	}
}

func (s *boundedStore) cleanup(now time.Time) {
	// Reclaim other inactive keys too: all shards are visited within 32 auth
	// attempts. During complete inactivity, retained state stays capped.
	shard := &s.shards[s.sweep.Add(1)%uint64(len(s.shards))]
	shard.Lock()
	shard.expire(now)
	shard.Unlock()
}

func clientNetwork(c *gin.Context) string {
	ip := c.ClientIP()
	// Caddy supplies one XFF field. Ambiguous repeated fields and excessive
	// proxy chains fall back to the socket peer. Never consult X-Real-IP here.
	forwarded := c.Request.Header.Values("X-Forwarded-For")
	if len(forwarded) > 1 || (len(forwarded) == 1 && len(forwarded[0]) > 4096) {
		ip = c.RemoteIP()
	}
	address, err := netip.ParseAddr(ip)
	if err != nil {
		return "unknown"
	}
	address = address.Unmap().WithZone("")
	if address.Is6() {
		return netip.PrefixFrom(address, 64).Masked().String()
	}
	return address.String()
}

func readAuthBody(c *gin.Context) ([]byte, bool) {
	if c.Request.Body == nil {
		return nil, true
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxAuthBodyBytes+1))
	if len(body) > maxAuthBodyBytes {
		c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
		return nil, false
	}
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return nil, false
	}
	// net/http closes the original body. Preserve all bytes for the handler,
	// including trailing input, within the enforced total size limit.
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body, true
}

func requestIdentity(body []byte, endpoint AuthEndpoint) string {
	// Use Gin's decoder and consume the first JSON value exactly as its binder
	// does. Only decode fields the endpoint uses: unrelated ill-typed fields must
	// not change the bucket selected for a request the handler accepts.
	var identity string
	decoder := json.API.NewDecoder(bytes.NewReader(body))
	switch endpoint {
	case LoginEndpoint, RegisterEndpoint:
		var value struct {
			Email string `json:"email"`
		}
		if decoder.Decode(&value) != nil {
			return ""
		}
		identity = strings.ToLower(strings.TrimSpace(value.Email))
	case RefreshEndpoint, LogoutEndpoint:
		var value struct {
			RefreshToken string `json:"refresh_token"`
		}
		if decoder.Decode(&value) != nil {
			return ""
		}
		identity = value.RefreshToken
	}
	if identity == "" {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(identity)))
}

func respondRateLimited(c *gin.Context, retryAfter time.Duration) {
	seconds := retryAfter / time.Second
	if retryAfter%time.Second != 0 {
		seconds++
	}
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.FormatInt(int64(seconds), 10))
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": rateLimitError})
}
