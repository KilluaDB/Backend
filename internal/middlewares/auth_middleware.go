package middlewares

import (
	"backend/internal/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func Authenticate(c *gin.Context) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Missing Authorization header"})
		return
	}

	// Expected format: "Bearer <token>"
	parts := strings.Split(authHeader, " ")
	if len(parts) != 2 || parts[0] != "Bearer" {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Invalid Authorization format"})
		return
	}

	tokenStr := parts[1]

	// Verify token using the same secret you used for generating access tokens
	claims, err := utils.VerifyJWT(tokenStr, utils.AccessTokenSecret)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Invalid or expired token"})
		return
	}

	// Store the user ID in context for handlers
	c.Set("userId", claims.UserID)

	c.Next()
}
