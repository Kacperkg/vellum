package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/services"
)

type GoalHandler struct {
	goalService *services.GoalService
}

func NewGoalHandler(goalService *services.GoalService) *GoalHandler {
	return &GoalHandler{
		goalService: goalService,
	}
}

func (h *GoalHandler) Create(c *gin.Context) {
	var req dto.CreateGoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.goalService.Create(userID, req)
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

	response, err := h.goalService.GetByID(userID, goalID)
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

	response, err := h.goalService.FindByUserID(userID)
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

	var req dto.UpdateGoalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.goalService.Update(userID, goalID, req)
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

	if err := h.goalService.Delete(userID, goalID); err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
