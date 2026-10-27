package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
)

func AuthMiddleware(secret string, validateVersion func(context.Context, uuid.UUID, int64) bool) gin.HandlerFunc {
	if validateVersion == nil {
		panic("authentication requires an auth version validator")
	}
	return func(c *gin.Context) {

		// Get Authorization header
		authHeader := c.GetHeader("Authorization")

		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "authorization header is required",
			})
			c.Abort()
			return
		}

		// Check Bearer token
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid authorization header",
			})
			c.Abort()
			return
		}

		// Extract token
		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		// Validate token
		claims, err := auth.ValidateToken(tokenString, secret)
		validVersion := false
		if err == nil {
			validVersion = validateVersion(c.Request.Context(), claims.UserID, claims.AuthVersion)
			if !resourceContextActive(c) {
				return
			}
		}
		if err != nil || !validVersion {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid or expired token",
			})
			c.Abort()
			return
		}

		// Store user ID in context
		c.Set("userID", claims.UserID)

		// Continue to next handler
		c.Next()
	}
}
