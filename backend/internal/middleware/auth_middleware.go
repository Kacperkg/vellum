package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/kacperkg/vellum/internal/auth"
)

func AuthMiddleware(secret string, validateSession func(context.Context, uuid.UUID, int64, *uuid.UUID) bool) gin.HandlerFunc {
	if validateSession == nil {
		panic("authentication requires a session validator")
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
		if err != nil || !validateSession(c.Request.Context(), claims.UserID, claims.AuthVersion, claims.SessionID) {
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
