package middlewares

import (
	"backend/internal/repositories"
	"backend/internal/responses"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RequirePostgresProject ensures the project exists, belongs to the user, and is PostgreSQL.
// Use after Authenticate. Expects project id in Param("id") and userId in context.
func RequirePostgresProject(projectRepo repositories.ProjectRepository) gin.HandlerFunc {
	return requireProjectDBType(projectRepo, "postgresql")
}

// RequireMongoProject ensures the project exists, belongs to the user, and is MongoDB.
func RequireMongoProject(projectRepo repositories.ProjectRepository) gin.HandlerFunc {
	return requireProjectDBType(projectRepo, "mongodb")
}

func requireProjectDBType(projectRepo repositories.ProjectRepository, wantDBType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID, exists := c.Get("userId")
		if !exists {
			responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
			c.Abort()
			return
		}
		var userUUID uuid.UUID
		switch v := userID.(type) {
		case uuid.UUID:
			userUUID = v
		case string:
			var err error
			userUUID, err = uuid.Parse(v)
			if err != nil {
				responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID")
				c.Abort()
				return
			}
		default:
			responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID")
			c.Abort()
			return
		}
		projectIDStr := c.Param("id")
		if projectIDStr == "" {
			responses.Fail(c, http.StatusBadRequest, nil, "Project ID is required")
			c.Abort()
			return
		}
		projectUUID, err := uuid.Parse(projectIDStr)
		if err != nil {
			responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID")
			c.Abort()
			return
		}
		project, err := projectRepo.GetByIDAndUserID(c.Request.Context(), projectUUID, userUUID)
		if err != nil || project == nil {
			responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			c.Abort()
			return
		}
		if project.DBType != wantDBType {
			responses.Fail(c, http.StatusBadRequest, nil, "This endpoint is only available for "+wantDBType+" projects")
			c.Abort()
			return
		}
		c.Next()
	}
}
