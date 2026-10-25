package routes

import (
	"fmt"
	"net/netip"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/config"
	"github.com/kacperkg/vellum/internal/middleware"
)

// NewRouter is shared by the server and middleware-chain tests so proxy, CORS
// and authentication protection cannot silently drift between them.
func NewRouter(cfg *config.Config, handlers HandlerSet) (*gin.Engine, error) {
	router := gin.Default()
	if err := ConfigureClientIP(router, cfg.TrustedProxyCIDRs); err != nil {
		return nil, err
	}
	router.Use(cors.New(cors.Config{
		AllowOrigins:  cfg.CORSAllowedOrigins,
		AllowMethods:  []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Authorization"},
		ExposeHeaders: []string{"Retry-After"},
	}))
	limiter := middleware.NewAuthRateLimiter(map[middleware.AuthEndpoint]middleware.RateLimit{
		middleware.LoginEndpoint:    {Requests: cfg.LoginRateLimit.Requests, Window: cfg.LoginRateLimit.Window},
		middleware.RegisterEndpoint: {Requests: cfg.RegisterRateLimit.Requests, Window: cfg.RegisterRateLimit.Window},
		middleware.RefreshEndpoint:  {Requests: cfg.RefreshRateLimit.Requests, Window: cfg.RefreshRateLimit.Window},
		middleware.LogoutEndpoint:   {Requests: cfg.LogoutRateLimit.Requests, Window: cfg.LogoutRateLimit.Window},
	}, cfg.RateLimitMaxEntries, cfg.RateLimitSharedIPMultiplier)
	Register(router, cfg.JWTSecret, handlers, limiter)
	return router, nil
}

// Caddy overwrites XFF for public requests. It does not sanitize X-Real-IP, so
// never let Gin fall back to that header or a platform-specific header.
func ConfigureClientIP(router *gin.Engine, proxies []string) error {
	for _, value := range proxies {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			if _, ipErr := netip.ParseAddr(value); ipErr != nil {
				return fmt.Errorf("invalid TRUSTED_PROXY_CIDRS entry %q", value)
			}
		} else if prefix.Bits() == 0 || (prefix.Addr().Is4In6() && prefix.Bits() <= 96) {
			return fmt.Errorf("TRUSTED_PROXY_CIDRS must not trust all clients: %q", value)
		}
	}
	if err := router.SetTrustedProxies(proxies); err != nil {
		return fmt.Errorf("invalid TRUSTED_PROXY_CIDRS: %w", err)
	}
	router.RemoteIPHeaders = []string{"X-Forwarded-For"}
	router.TrustedPlatform = ""
	router.AppEngine = false
	router.ForwardedByClientIP = true
	return nil
}
