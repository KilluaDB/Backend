package handlers

import (
	"fmt"
	"my_project/internal/responses"
	"my_project/internal/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

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

	project, err := h.projectService.CreateProject(userIDStr, req)
	if err != nil {
		fmt.Printf("ERROR in CreateProject handler: %v\n", err)
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to create project")
		return
	}

	responses.Success(c, http.StatusCreated, project, "Project created successfully")
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

	// Get project and verify it belongs to the authenticated user
	project, err := h.projectService.GetProjectByIDAndUserID(projectID, userIDStr)
	if err != nil {
		responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
		return
	}

	responses.Success(c, http.StatusOK, project, "Project retrieved successfully")
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

	responses.Success(c, http.StatusOK, projects, "Projects retrieved successfully")
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

	// Delete project and verify it belongs to the authenticated user
	err := h.projectService.DeleteProjectByIDAndUserID(projectID, userIDStr)
	if err != nil {
		responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
		return
	}

	responses.Success(c, http.StatusOK, nil, "Project deleted successfully")
}

