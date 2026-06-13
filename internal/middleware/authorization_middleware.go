package middleware

import (
	"backend/internal/repository"
	"backend/internal/utils"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireAdmin checks if the authenticated user is an admin
// This middleware should be used after Authenticate middleware
func RequireAdmin(userRepo repository.UserStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get authenticated user ID from context (set by Authenticate middleware)
		authenticatedUserID, ok := utils.UserIDFromGin(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"message": "Unauthorized"})
			return
		}

		// Get authenticated user to check their role
		authenticatedUser, err := userRepo.FindUserByID(c, authenticatedUserID)
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
