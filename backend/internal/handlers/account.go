package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/services"
)

type AccountHandler struct {
	accountService *services.AccountService
}

type accountRequest struct {
	Name string `json:"name" binding:"required,max=200"`
}

func NewAccountHandler(accountService *services.AccountService) *AccountHandler {
	return &AccountHandler{
		accountService: accountService,
	}
}

func (h *AccountHandler) Create(c *gin.Context) {
	var req accountRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		respondWithBindingError(c, err)
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.accountService.WithContext(c.Request.Context()).Create(userID, services.CreateAccountRequest(req))
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

	response, err := h.accountService.WithContext(c.Request.Context()).GetByID(userID, accountID)
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

	accounts, err := h.accountService.WithContext(c.Request.Context()).FindByUserID(userID)
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

	var req accountRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		respondWithBindingError(c, err)
		return
	}

	userID, ok := getUserID(c)
	if !ok {
		return
	}

	response, err := h.accountService.WithContext(c.Request.Context()).Update(userID, accountID, services.UpdateAccountRequest(req))
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

	err := h.accountService.WithContext(c.Request.Context()).Delete(userID, accountID)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}
