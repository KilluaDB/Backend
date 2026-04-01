package middlewares

import (
	"backend/internal/repositories"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequireAdmin checks if the authenticated user is an admin
// This middleware should be used after Authenticate middleware
func RequireAdmin(userRepo *repositories.UserRepository) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get authenticated user ID from context (set by Authenticate middleware)
		userID, exists := c.Get("userId")
		if !exists {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}

		// Convert userID to UUID
		var authenticatedUserID uuid.UUID
		switch v := userID.(type) {
		case uuid.UUID:
			authenticatedUserID = v
		case string:
			parsed, err := uuid.Parse(v)
			if err != nil {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Invalid user ID format"})
				return
			}
			authenticatedUserID = parsed
		default:
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Invalid user ID format"})
			return
		}

		// Get authenticated user to check their role
		authenticatedUser, err := userRepo.FindUserByID(authenticatedUserID)
		if err != nil || authenticatedUser == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "User not found"})
			return
		}

		// Check if user is an admin
		if authenticatedUser.Role != "admin" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"message": "Access denied. Admin privileges required."})
			return
		}

		// Store the authenticated user in context for handlers to use
		c.Set("authenticatedUser", authenticatedUser)
		c.Next()
	}
}
