package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/services"
)

type GoalHandler struct {
	goalService *services.GoalService
}

type goalRequest struct {
	AccountID           *uuid.UUID `json:"account_id"`
	Name                string     `json:"name" binding:"required,max=200"`
	TargetAmount        int64      `json:"target_amount" binding:"required,gt=0"`
	SavedAmount         int64      `json:"saved_amount" binding:"gte=0"`
	MonthlyContribution int64      `json:"monthly_contribution" binding:"gte=0"`
}

func NewGoalHandler(goalService *services.GoalService) *GoalHandler {
	return &GoalHandler{
		goalService: goalService,
	}
}

func (h *GoalHandler) Create(c *gin.Context) {
	var req goalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithBindingError(c, err)
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.goalService.WithContext(c.Request.Context()).Create(userID, services.CreateGoalRequest(req))
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

func (h *GoalHandler) GetByID(c *gin.Context) {
	goalID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.goalService.WithContext(c.Request.Context()).GetByID(userID, goalID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *GoalHandler) ListByUser(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.goalService.WithContext(c.Request.Context()).FindByUserID(userID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *GoalHandler) Update(c *gin.Context) {
	goalID, ok := getIDParam(c)
	if !ok {
		return
	}

	var req goalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithBindingError(c, err)
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.goalService.WithContext(c.Request.Context()).Update(userID, goalID, services.UpdateGoalRequest(req))
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *GoalHandler) Delete(c *gin.Context) {
	goalID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	if err := h.goalService.WithContext(c.Request.Context()).Delete(userID, goalID); err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
