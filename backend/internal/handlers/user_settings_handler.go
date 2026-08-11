package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/services"
)

type UserSettingsHandler struct {
	userSettingsService *services.UserSettingsService
}

func NewUserSettingsHandler(service *services.UserSettingsService) *UserSettingsHandler {
	return &UserSettingsHandler{userSettingsService: service}
}

func (h *UserSettingsHandler) Get(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.userSettingsService.Get(userID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *UserSettingsHandler) Update(c *gin.Context) {
	var req dto.UpdateUserSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.userSettingsService.Update(userID, req)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}
