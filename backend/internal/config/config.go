package config

import (
	"log"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

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
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	return &Config{
		Port:               required("PORT"),
		BindHost:           os.Getenv("BIND_HOST"),
		CORSAllowedOrigins: corsOrigins(os.Getenv("CORS_ALLOWED_ORIGINS")),
		DBHost:             required("DB_HOST"),
		DBPort:             required("DB_PORT"),
		DBUser:             required("DB_USER"),
		DBPassword:         required("DB_PASSWORD"),
		DBName:             required("DB_NAME"),
		DBSSLMode:          required("DB_SSLMODE"),
		JWTSecret:          required("JWT_SECRET"),
		JWTExpiry:          required("JWT_EXPIRY"),
		JWTRefreshExpiry:   required("JWT_REFRESH_EXPIRY"),
	}
}

func corsOrigins(value string) []string {
	origins := make([]string, 0)
	for _, origin := range strings.Split(value, ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			origins = append(origins, origin)
		}
	}
	if len(origins) == 0 {
		return []string{"http://localhost:5173"}
	}
	return origins
}

func required(key string) string {
	value := os.Getenv(key)

	if value == "" {
		log.Fatalf("%s is required", key)
	}

	return value
}
