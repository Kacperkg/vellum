package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
)

type UserSettingsHandler struct {
	userSettingsService *services.UserSettingsService
}

type userSettingsRequest struct {
	Currency     models.Currency     `json:"currency" binding:"required,oneof=GBP USD EUR"`
	DateFormat   models.DateFormat   `json:"date_format" binding:"required,oneof='DD/MM/YYYY' 'MM/DD/YYYY' 'YYYY-MM-DD'"`
	NumberFormat models.NumberFormat `json:"number_format" binding:"required,oneof=comma_decimal dot_decimal apostrophe"`
	WeekStartsOn models.WeekStartsOn `json:"week_starts_on" binding:"required,oneof=Monday Sunday"`
}

func NewUserSettingsHandler(service *services.UserSettingsService) *UserSettingsHandler {
	return &UserSettingsHandler{userSettingsService: service}
}

func (h *UserSettingsHandler) Get(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.userSettingsService.WithContext(c.Request.Context()).Get(userID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *UserSettingsHandler) Update(c *gin.Context) {
	var req userSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithBindingError(c, err)
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.userSettingsService.WithContext(c.Request.Context()).Update(userID, services.UpdateUserSettingsRequest(req))
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}
