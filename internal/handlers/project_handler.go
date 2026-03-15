package handlers

import (
	"backend/internal/models"
	"backend/internal/postgres/service"
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
	case "postgresql", "sql":
		return "sql"
	case "mongodb", "nosql":
		return "nosql"
	default:
		return dbType
	}
}

func projectToAPI(p *models.Project) gin.H {
	return gin.H{
		"id":            p.ID,
		"user_id":       p.UserID,
		"name":          p.Name,
		"description":   p.Description,
		"db_type":       dbTypeForAPI(p.DBType),
		"resource_tier": p.ResourceTier,
		"created_at":    p.CreatedAt,
		"status":        p.Status,
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

	project, instance, err := h.projectService.CreateProject(userIDStr, req)
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
	projectData["status"] = instance.Status

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

// InsertRow handles POST /api/v1/projects/:id/tables/:table_name/rows
func (h *ProjectHandler) InsertRow(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectID := c.Param("id")

	// Convert userID to UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
		return
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID format")
		return
	}

	var req service.InsertRowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if req.Table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required in the request body")
		return
	}

	result, err := h.projectService.InsertRow(userUUID, projectUUID, req)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			return
		}
		if errors.Is(err, services.ErrUnsupportedDBForRows) {
			responses.Fail(c, http.StatusBadRequest, err, "Row and column operations are only supported for PostgreSQL projects")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to insert row")
		return
	}

	responses.Success(c, http.StatusCreated, result, "Row inserted successfully")
}

// DeleteRow handles DELETE /api/v1/projects/:id/rows/:row_id
func (h *ProjectHandler) DeleteRow(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectID := c.Param("id")
	rowID := c.Param("row_id")

	// Convert userID to UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
		return
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID format")
		return
	}

	var req service.DeleteRowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	err = h.projectService.DeleteRow(userUUID, projectUUID, req, rowID)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			return
		}
		if errors.Is(err, services.ErrUnsupportedDBForRows) {
			responses.Fail(c, http.StatusBadRequest, err, "Row and column operations are only supported for PostgreSQL projects")
			return
		}
		if err.Error() == "row not found" {
			responses.Fail(c, http.StatusNotFound, err, "Row not found")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete row")
		return
	}

	responses.Success(c, http.StatusNoContent, nil, "Row deleted successfully")
}

// AddColumn handles POST /api/v1/projects/:id/columns
func (h *ProjectHandler) AddColumn(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectID := c.Param("id")

	// Convert userID to UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
		return
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID format")
		return
	}

	var req service.AddColumnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	result, err := h.projectService.AddColumn(userUUID, projectUUID, req)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			return
		}
		if errors.Is(err, services.ErrUnsupportedDBForRows) {
			responses.Fail(c, http.StatusBadRequest, err, "Row and column operations are only supported for PostgreSQL projects")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to add column")
		return
	}

	responses.Success(c, http.StatusOK, result, "Column added successfully")
}

// DeleteColumn handles DELETE /api/v1/projects/:id/columns/:column_name
func (h *ProjectHandler) DeleteColumn(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectID := c.Param("id")
	columnName := c.Param("column_name")

	// Convert userID to UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
		return
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID format")
		return
	}

	var req service.DeleteColumnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	err = h.projectService.DeleteColumn(userUUID, projectUUID, req, columnName)
	if err != nil {
		if errors.Is(err, services.ErrProjectNotFound) {
			responses.Fail(c, http.StatusNotFound, err, "Project not found or access denied")
			return
		}
		if errors.Is(err, services.ErrUnsupportedDBForRows) {
			responses.Fail(c, http.StatusBadRequest, err, "Row and column operations are only supported for PostgreSQL projects")
			return
		}
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to delete column")
		return
	}

	responses.Success(c, http.StatusNoContent, nil, "Column deleted successfully")
}
