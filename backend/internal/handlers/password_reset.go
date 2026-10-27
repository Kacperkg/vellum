package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kacperkg/vellum/internal/services"
)

type PasswordResetHandler struct {
	service *services.PasswordResetService
}

func NewPasswordResetHandler(service *services.PasswordResetService) *PasswordResetHandler {
	return &PasswordResetHandler{service: service}
}

func recoveryHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
}
func (h *PasswordResetHandler) ForgotPassword(c *gin.Context) {
	recoveryHeaders(c)
	var req struct {
		Email string `json:"email" binding:"required,email,max=254"`
	}
	// Validation failures describe syntax only, never database or provider state.
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide a valid email address"})
		return
	}
	if h != nil && h.service != nil {
		h.service.QueueRecovery(strings.TrimSpace(req.Email))
	}
	c.JSON(http.StatusOK, gin.H{"message": services.RecoveryMessage})
}
func (h *PasswordResetHandler) ResetPassword(c *gin.Context) {
	recoveryHeaders(c)
	var req struct {
		Token    string `json:"token" binding:"required,len=43"`
		Password string `json:"new_password" binding:"required,min=8,max=72"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "provide a valid reset link and a password of 8 to 72 characters (at most 72 UTF-8 bytes)"})
		return
	}
	if h == nil || h.service == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": services.ErrInvalidResetToken.Error()})
		return
	}
	if err := h.service.Reset(c.Request.Context(), req.Token, req.Password); err != nil {
		if errors.Is(err, services.ErrInvalidResetToken) {
			c.JSON(http.StatusBadRequest, gin.H{"error": services.ErrInvalidResetToken.Error()})
			return
		}
		respondWithServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Your password has been reset. Please sign in with your new password."})
}
