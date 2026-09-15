package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/services"
)

type BudgetHandler struct {
	budgetService *services.BudgetService
}

type budgetRequest struct {
	CategoryID      uuid.UUID `json:"category_id" binding:"required"`
	AllocatedAmount int64     `json:"allocated_amount" binding:"required,gt=0"`
}

func NewBudgetHandler(budgetService *services.BudgetService) *BudgetHandler {
	return &BudgetHandler{
		budgetService: budgetService,
	}
}

func (h *BudgetHandler) Create(c *gin.Context) {
	var req budgetRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.budgetService.Create(userID, services.CreateBudgetRequest(req))
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

func (h *BudgetHandler) Update(c *gin.Context) {
	budgetID, ok := getIDParam(c)
	if !ok {
		return
	}

	var req budgetRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.budgetService.Update(userID, budgetID, services.UpdateBudgetRequest(req))
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *BudgetHandler) FindByID(c *gin.Context) {
	budgetID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.budgetService.FindByID(userID, budgetID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *BudgetHandler) ListByUser(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.budgetService.FindByUserID(userID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *BudgetHandler) Delete(c *gin.Context) {
	budgetID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	err := h.budgetService.Delete(userID, budgetID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
