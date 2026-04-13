package handlers

import (
	"backend/internal/models"
	"backend/internal/responses"
	"backend/internal/services"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// dbTypeForAPI normalizes db_type for API responses.
func dbTypeForAPI(dbType string) string {
	switch dbType {
	case "postgres", "postgresql", "sql":
		return "sql"
	case "mongodb", "nosql":
		return "nosql"
	default:
		return dbType
	}
}

func projectToAPI(p *models.Project) gin.H {
	return gin.H{
		"id":                 p.ID,
		"user_id":            p.UserID,
		"name":               p.Name,
		"description":        p.Description,
		"db_type":            dbTypeForAPI(p.DBType),
		"resource_tier":      p.ResourceTier,
		"created_at":         p.CreatedAt,
		"status":             p.Status,
		"runtime_created_at": p.RuntimeCreatedAt,
		"runtime_updated_at": p.RuntimeUpdatedAt,
	}
}

type ProjectHandler struct {
	projectService *services.ProjectService
}

func NewProjectHandler(projectService *services.ProjectService) *ProjectHandler {
	return &ProjectHandler{
		projectService: projectService,
	}
}

// CreateProject handles POST /api/v1/projects
func (h *ProjectHandler) CreateProject(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req services.CreateProjectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	// Convert userID to string (it's a uuid.UUID from the JWT claims)
	userIDStr := ""
	switch v := userID.(type) {
	case uuid.UUID:
		userIDStr = v.String()
	case string:
		userIDStr = v
	default:
		userIDStr = fmt.Sprintf("%v", v)
	}

	project, _, err := h.projectService.CreateProject(userIDStr, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidUserID):
			responses.Fail(c, http.StatusBadRequest, err, "Invalid user context")
			return
		case errors.Is(err, services.ErrInvalidDBType):
			responses.Fail(c, http.StatusBadRequest, err, "Invalid db_type: must be 'postgresql', 'sql', 'mongodb', or 'nosql'")
			return
		case errors.Is(err, services.ErrInvalidResourceTier):
			responses.Fail(c, http.StatusBadRequest, err, "Invalid resource_tier: must be 'free', 'basic', or 'premium'")
			return
		case errors.Is(err, services.ErrProjectCreateDB):
			responses.Fail(c, http.StatusInternalServerError, err, "Failed to create project or database instance")
			return
		default:
			responses.Fail(c, http.StatusInternalServerError, err, "Failed to create project")
			return
		}
	}

	projectData := projectToAPI(project)

	responseData := gin.H{
		"project": projectData,
	}

	responses.Success(c, http.StatusCreated, responseData, "Project created successfully")
}

// GetProject handles GET /api/v1/projects/:id
func (h *ProjectHandler) GetProject(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectID := c.Param("id")

	// Convert userID to string
	userIDStr := ""
	switch v := userID.(type) {
	case uuid.UUID:
		userIDStr = v.String()
	case string:
		userIDStr = v
	default:
		userIDStr = fmt.Sprintf("%v", v)
	}

	project, err := h.projectService.GetProjectByIDAndUserID(projectID, userIDStr)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) || errors.Is(err, services.ErrInvalidProjectID) || errors.Is(err, services.ErrInvalidUserID) {
			responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve project")
		return
	}

	responses.Success(c, http.StatusOK, projectToAPI(project), "Project retrieved successfully")
}

// ListProjects handles GET /api/v1/projects
func (h *ProjectHandler) ListProjects(c *gin.Context) {
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	userIDStr := ""
	switch v := userID.(type) {
	case uuid.UUID:
		userIDStr = v.String()
	case string:
		userIDStr = v
	default:
		userIDStr = fmt.Sprintf("%v", v)
	}

	projects, err := h.projectService.GetProjectsByUserID(userIDStr)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve projects")
		return
	}

	list := make([]gin.H, 0, len(projects))
	for i := range projects {
		list = append(list, projectToAPI(&projects[i]))
	}
	responses.Success(c, http.StatusOK, list, "Projects retrieved successfully")
}

// DeleteProject handles DELETE /api/v1/projects/:id
func (h *ProjectHandler) DeleteProject(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectID := c.Param("id")

	// Convert userID to string
	userIDStr := ""
	switch v := userID.(type) {
	case uuid.UUID:
		userIDStr = v.String()
	case string:
		userIDStr = v
	default:
		userIDStr = fmt.Sprintf("%v", v)
	}

	err := h.projectService.DeleteProjectByIDAndUserID(projectID, userIDStr)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrProjectNotFound), errors.Is(err, services.ErrInvalidProjectID), errors.Is(err, services.ErrInvalidUserID):
			responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			return
		default:
			responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete project")
			return
		}
	}

	responses.Success(c, http.StatusOK, nil, "Project deleted successfully")
}
