package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	Port string

	DBHost     string
	DBPort     string
	DBUser     string
	DBPassword string
	DBName     string
	DBSSLMode  string

	JWTSecret string
	JWTExpiry string
}

func Load() *Config {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found")
	}

	return &Config{
		Port:       required("PORT"),
		DBHost:     required("DB_HOST"),
		DBPort:     required("DB_PORT"),
		DBUser:     required("DB_USER"),
		DBPassword: required("DB_PASSWORD"),
		DBName:     required("DB_NAME"),
		DBSSLMode:  required("DB_SSLMODE"),
		JWTSecret:  required("JWT_SECRET"),
		JWTExpiry:  required("JWT_EXPIRY"),
	}
}

func required(key string) string {
	value := os.Getenv(key)

	if value == "" {
		log.Fatalf("%s is required", key)
	}

	return value
}
