package dto

import (
	"github.com/google/uuid"
)

type CreateAccountRequest struct {
	Name string `json:"name" binding:"required"`
}

type UpdateAccountRequest struct {
	Name string `json:"name" binding:"required"`
}

type AccountResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
