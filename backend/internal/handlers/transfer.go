package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/calendar"
	"github.com/kacperkg/vellum/internal/models"
	"github.com/kacperkg/vellum/internal/services"
)

type TransferHandler struct{ transferService *services.TransferService }

type transferRequest struct {
	Name          string            `json:"name" binding:"required"`
	Amount        int64             `json:"amount" binding:"required,gt=0"`
	FromAccountID uuid.UUID         `json:"from_account_id" binding:"required"`
	ToAccountID   uuid.UUID         `json:"to_account_id" binding:"required"`
	Date          calendar.Date     `json:"date" binding:"required"`
	Frequency     *models.Frequency `json:"frequency" binding:"omitempty,oneof=one-off daily weekly monthly yearly"`
}

func (r transferRequest) serviceRequest() services.CreateTransferRequest {
	frequency := models.OneOff
	if r.Frequency != nil {
		frequency = *r.Frequency
	}
	return services.CreateTransferRequest{Name: r.Name, Amount: r.Amount, FromAccountID: r.FromAccountID, ToAccountID: r.ToAccountID, Date: r.Date.Time, Frequency: frequency}
}

func NewTransferHandler(service *services.TransferService) *TransferHandler {
	return &TransferHandler{transferService: service}
}

func (h *TransferHandler) Create(c *gin.Context) {
	var req transferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID, ok := getUserID(c)
	if !ok {
		return
	}
	response, err := h.transferService.Create(userID, req.serviceRequest())
	if err != nil {
		respondWithServiceError(c, err)
		return
	}
	c.JSON(http.StatusCreated, response)
}

func (h *TransferHandler) GetByID(c *gin.Context) {
	id, ok := getIDParam(c)
	if !ok {
		return
	}
	userID, ok := getUserID(c)
	if !ok {
		return
	}
	response, err := h.transferService.GetByID(userID, id)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *TransferHandler) ListByUser(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}
	response, err := h.transferService.FindByUserID(userID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *TransferHandler) Update(c *gin.Context) {
	id, ok := getIDParam(c)
	if !ok {
		return
	}
	var req transferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	userID, ok := getUserID(c)
	if !ok {
		return
	}
	response, err := h.transferService.Update(userID, id, req.serviceRequest())
	if err != nil {
		respondWithServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, response)
}

func (h *TransferHandler) Delete(c *gin.Context) {
	id, ok := getIDParam(c)
	if !ok {
		return
	}
	userID, ok := getUserID(c)
	if !ok {
		return
	}
	if err := h.transferService.Delete(userID, id); err != nil {
		respondWithServiceError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
