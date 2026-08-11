package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/services"
)

type BudgetHandler struct {
	budgetService *services.BudgetService
}

func NewBudgetHandler(budgetService *services.BudgetService) *BudgetHandler {
	return &BudgetHandler{
		budgetService: budgetService,
	}
}

func (h *BudgetHandler) Create(c *gin.Context) {
	var req dto.CreateBudgetRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.budgetService.Create(userID, req)
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

	var req dto.UpdateBudgetRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.budgetService.Update(userID, budgetID, req)
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
