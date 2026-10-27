package handlers

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
)

type TransactionHandler struct {
	transactionService *services.TransactionService
}

type transactionRequest struct {
	Name       string                 `json:"name" binding:"required,max=200"`
	Amount     int64                  `json:"amount" binding:"required,gt=0"`
	AccountID  uuid.UUID              `json:"account_id" binding:"required"`
	CategoryID uuid.UUID              `json:"category_id" binding:"required"`
	Date       time.Time              `json:"date" binding:"required"`
	Frequency  models.Frequency       `json:"frequency" binding:"required,oneof=one-off daily weekly monthly yearly"`
	Type       models.TransactionType `json:"type" binding:"required,oneof=income expense"`
}

func NewTransactionHandler(transactionService *services.TransactionService) *TransactionHandler {
	return &TransactionHandler{
		transactionService: transactionService,
	}
}

func (h *TransactionHandler) Create(c *gin.Context) {
	var req transactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithBindingError(c, err)
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.transactionService.WithContext(c.Request.Context()).Create(userID, services.CreateTransactionRequest(req))
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

func (h *TransactionHandler) GetByID(c *gin.Context) {
	transactionID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.transactionService.WithContext(c.Request.Context()).GetByID(userID, transactionID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *TransactionHandler) ListByUser(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.transactionService.WithContext(c.Request.Context()).FindByUserID(userID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *TransactionHandler) Update(c *gin.Context) {
	transactionID, ok := getIDParam(c)
	if !ok {
		return
	}

	var req transactionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondWithBindingError(c, err)
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.transactionService.WithContext(c.Request.Context()).Update(userID, transactionID, services.UpdateTransactionRequest(req))
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *TransactionHandler) Delete(c *gin.Context) {
	transactionID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	if err := h.transactionService.WithContext(c.Request.Context()).Delete(userID, transactionID); err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *TransactionHandler) ListByAccount(c *gin.Context) {
	accountID, ok := getUUIDParam(c, "account_id")
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.transactionService.WithContext(c.Request.Context()).FindByAccountID(userID, accountID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *TransactionHandler) ListByCategory(c *gin.Context) {
	categoryID, ok := getUUIDParam(c, "category_id")
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.transactionService.WithContext(c.Request.Context()).FindByCategoryID(userID, categoryID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *TransactionHandler) ListByDateRange(c *gin.Context) {
	startDate, err := time.Parse(time.RFC3339, c.Query("start_date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "start_date must be an RFC3339 timestamp"})
		return
	}

	endDate, err := time.Parse(time.RFC3339, c.Query("end_date"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "end_date must be an RFC3339 timestamp"})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.transactionService.WithContext(c.Request.Context()).FindByDateRange(userID, startDate, endDate)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *TransactionHandler) ListByFrequency(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	frequency := models.Frequency(c.Query("frequency"))
	response, err := h.transactionService.WithContext(c.Request.Context()).FindByFrequency(userID, frequency)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}
