package middleware

import (
	"backend/internal/repository"
	"backend/internal/response"
	"backend/internal/utils"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// RequirePostgresProject ensures the project exists, belongs to the user, and is PostgreSQL.
// Use after Authenticate. Expects project id in Param("id") and userId in context.
func RequirePostgresProject(projectRepo *repository.ProjectRepository) gin.HandlerFunc {
	return requireProjectDBType(projectRepo, "postgresql")
}

// RequireMongoProject ensures the project exists, belongs to the user, and is MongoDB.
func RequireMongoProject(projectRepo *repository.ProjectRepository) gin.HandlerFunc {
	return requireProjectDBType(projectRepo, "mongodb")
}

func requireProjectDBType(projectRepo *repository.ProjectRepository, wantDBType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userUUID, projectUUID, ok, projErr := utils.UserAndProjectFromGin(c)
		if !ok {
			if projErr != nil {
				response.Fail(c, http.StatusBadRequest, projErr, "Invalid project ID")
			} else {
				response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
			}
			c.Abort()
			return
		}
		project, err := projectRepo.GetByIDAndUserID(c.Request.Context(), projectUUID, userUUID)
		if err != nil || project == nil {
			response.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			c.Abort()
			return
		}
		if canonicalProjectDBType(project.DBType) != canonicalProjectDBType(wantDBType) {
			response.Fail(c, http.StatusBadRequest, nil, "This endpoint is only available for "+wantDBType+" projects")
			c.Abort()
			return
		}
		c.Next()
	}
}

func canonicalProjectDBType(dbType string) string {
	switch strings.ToLower(strings.TrimSpace(dbType)) {
	case "postgres", "postgresql", "sql":
		return "postgresql"
	case "mongodb", "nosql":
		return "mongodb"
	default:
		return strings.ToLower(strings.TrimSpace(dbType))
	}
}
