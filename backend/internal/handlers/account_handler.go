package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/services"
)

type AccountHandler struct {
	accountService *services.AccountService
}

func NewAccountHandler(accountService *services.AccountService) *AccountHandler {
	return &AccountHandler{
		accountService: accountService,
	}
}

func (h *AccountHandler) Create(c *gin.Context) {
	var req dto.CreateAccountRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.accountService.Create(userID, req)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

func (h *AccountHandler) GetByID(c *gin.Context) {
	accountID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.accountService.GetByID(userID, accountID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AccountHandler) ListByUser(c *gin.Context) {
	userID, ok := getUserID(c)
	if !ok {
		return
	}

	accounts, err := h.accountService.FindByUserID(userID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, accounts)
}

func (h *AccountHandler) Update(c *gin.Context) {
	accountID, ok := getIDParam(c)
	if !ok {
		return
	}

	var req dto.UpdateAccountRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.accountService.Update(userID, accountID, req)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

func (h *AccountHandler) Delete(c *gin.Context) {
	accountID, ok := getIDParam(c)
	if !ok {
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	err := h.accountService.Delete(userID, accountID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
