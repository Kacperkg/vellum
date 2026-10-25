package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type RateLimit struct {
	Requests int
	Window   time.Duration
}

type Config struct {
	Port               string
	BindHost           string
	CORSAllowedOrigins []string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	JWTSecret        string
	JWTExpiry        string
	JWTRefreshExpiry string

	TrustedProxyCIDRs           []string
	LoginRateLimit              RateLimit
	RegisterRateLimit           RateLimit
	RefreshRateLimit            RateLimit
	LogoutRateLimit             RateLimit
	RateLimitMaxEntries         int
	RateLimitSharedIPMultiplier int
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	return &Config{
		Port:                        required("PORT"),
		BindHost:                    os.Getenv("BIND_HOST"),
		CORSAllowedOrigins:          corsOrigins(os.Getenv("CORS_ALLOWED_ORIGINS")),
		DBHost:                      required("DB_HOST"),
		DBPort:                      required("DB_PORT"),
		DBUser:                      required("DB_USER"),
		DBPassword:                  required("DB_PASSWORD"),
		DBName:                      required("DB_NAME"),
		DBSSLMode:                   required("DB_SSLMODE"),
		JWTSecret:                   required("JWT_SECRET"),
		JWTExpiry:                   required("JWT_EXPIRY"),
		JWTRefreshExpiry:            required("JWT_REFRESH_EXPIRY"),
		TrustedProxyCIDRs:           csvValues(os.Getenv("TRUSTED_PROXY_CIDRS")),
		LoginRateLimit:              rateLimit("LOGIN", 10, time.Minute),
		RegisterRateLimit:           rateLimit("REGISTER", 5, time.Hour),
		RefreshRateLimit:            rateLimit("REFRESH", 30, time.Minute),
		LogoutRateLimit:             rateLimit("LOGOUT", 30, time.Minute),
		RateLimitMaxEntries:         positiveInt("RATE_LIMIT_MAX_ENTRIES", 10000),
		RateLimitSharedIPMultiplier: positiveInt("RATE_LIMIT_SHARED_IP_MULTIPLIER", 20),
	}
}

func corsOrigins(value string) []string {
	origins := csvValues(value)
	if len(origins) == 0 {
		return []string{"http://localhost:5173"}
	}
	return origins
}

func csvValues(value string) []string {
	values := make([]string, 0)
	for _, origin := range strings.Split(value, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			values = append(values, origin)
		}
	}
	return values
}

func rateLimit(name string, defaultRequests int, defaultWindow time.Duration) RateLimit {
	requests := positiveInt("RATE_LIMIT_"+name+"_REQUESTS", defaultRequests)
	windowValue := valueOrDefault("RATE_LIMIT_"+name+"_WINDOW", defaultWindow.String())
	window, err := time.ParseDuration(windowValue)
	if err != nil || window <= 0 {
		log.Fatalf("RATE_LIMIT_%s_WINDOW must be a positive Go duration", name)
	}
	return RateLimit{Requests: requests, Window: window}
}

func positiveInt(key string, fallback int) int {
	value := valueOrDefault(key, strconv.Itoa(fallback))
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		log.Fatalf("%s must be a positive integer", key)
	}
	return parsed
}

func valueOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func required(key string) string {
	value := os.Getenv(key)

	if value == "" {
		log.Fatalf("%s is required", key)
	}

	return value
}
