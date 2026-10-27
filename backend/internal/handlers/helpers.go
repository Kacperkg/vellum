package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/services"
)

func getUserID(c *gin.Context) (userID uuid.UUID, ok bool) {
	userIDValue, exists := c.Get("userID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	userID, ok = userIDValue.(uuid.UUID)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	return userID, ok
}

func getIDParam(c *gin.Context) (id uuid.UUID, ok bool) {
	return getUUIDParam(c, "id")
}

func getUUIDParam(c *gin.Context, name string) (id uuid.UUID, ok bool) {
	idParam := c.Param(name)

	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	return id, true
}

func respondWithServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "request timed out"})
	case errors.Is(err, context.Canceled):
		c.JSON(http.StatusRequestTimeout, gin.H{"error": "request canceled"})
	case errors.Is(err, services.ErrUnauthorized):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrInvalidInput):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

// Binding errors never echo parser internals or user-controlled values.
func respondWithBindingError(c *gin.Context, err error) {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
}
