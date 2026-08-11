package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/dto"
	"github.com/kacperkg/vellum/internal/services"
)

type AuthHandler struct {
	userService *services.UserService
}

func NewAuthHandler(userService *services.UserService) *AuthHandler {
	return &AuthHandler{
		userService: userService,
	}
}

func (h *AuthHandler) Register(c *gin.Context) {

	var req dto.RegisterRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.userService.Register(req)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusCreated, response)
}

func (h *AuthHandler) Login(c *gin.Context) {

	var req dto.LoginRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response, err := h.userService.Login(req)
	if err != nil {
		respondWithServiceError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}
