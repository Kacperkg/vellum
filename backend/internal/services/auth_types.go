package services

import (
	"time"

	"github.com/google/uuid"
)

type RegisterRequest struct {
	FirstName string
	LastName  string
	Email     string
	Password  string
}

type LoginRequest struct {
	Email    string
	Password string
}

type AuthResponse struct {
	AccessToken  string       `json:"access_token"`
	RefreshToken string       `json:"refresh_token"`
	User         UserResponse `json:"user"`
}

type UserResponse struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
}

type RefreshRequest struct {
	RefreshToken string
}
