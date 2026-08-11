package handlers

import (
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
	idParam := c.Param("id")

	id, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad request"})
		return
	}

	return id, true
}

func respondWithServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrAccountNotFound),
		errors.Is(err, services.ErrBudgetNotFound),
		errors.Is(err, services.ErrCategoryNotFound),
		errors.Is(err, services.ErrGoalNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrUserEmailExists):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrAccountNameRequired),
		errors.Is(err, services.ErrAllocatedAmountPositive),
		errors.Is(err, services.ErrCategoryRequired),
		errors.Is(err, services.ErrGoalNameRequired),
		errors.Is(err, services.ErrGoalTargetPositive),
		errors.Is(err, services.ErrGoalSavedAmountNegative),
		errors.Is(err, services.ErrGoalSavedExceedsTarget),
		errors.Is(err, services.ErrGoalContributionNegative):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
