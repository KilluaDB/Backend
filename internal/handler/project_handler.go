package handler

import (
	"backend/internal/model"
	"backend/internal/response"
	"backend/internal/utils"
	"backend/internal/service"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// dbTypeForAPI normalizes db_type for API response.
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

func projectToAPI(p *model.Project) gin.H {
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
	projectService *service.ProjectService
}

func NewProjectHandler(projectService *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{
		projectService: projectService,
	}
}

// CreateProject handles POST /api/v1/projects
func (h *ProjectHandler) CreateProject(ctx *gin.Context) {
	// Get user ID from context
	userUUID, ok := utils.UserIDFromGin(ctx)
	if !ok {
		response.Fail(ctx, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	userIDStr := userUUID.String()

	var req service.CreateProjectRequest
	if err := ctx.ShouldBindJSON(&req); err != nil {
		response.Fail(ctx, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	project, err := h.projectService.CreateProject(ctx, userIDStr, req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidUserID):
			response.Fail(ctx, http.StatusBadRequest, err, "Invalid user context")
			return
		case errors.Is(err, service.ErrInvalidDBPassword):
			response.Fail(ctx, http.StatusBadRequest, err, "Invalid password: must be at least 12 characters with uppercase, lowercase, digits, and special characters")
			return
		case errors.Is(err, service.ErrInvalidDBType):
			response.Fail(ctx, http.StatusBadRequest, err, "Invalid db_type: must be 'postgresql', 'sql', 'mongodb', or 'nosql'")
			return
		case errors.Is(err, service.ErrInvalidResourceTier):
			response.Fail(ctx, http.StatusBadRequest, err, "Invalid resource_tier: must be 'free', 'basic', or 'premium'")
			return
		case errors.Is(err, service.ErrProjectCreateDB):
			response.Fail(ctx, http.StatusInternalServerError, err, "Failed to create project or database instance")
			return
		default:
			response.Fail(ctx, http.StatusInternalServerError, err, "Failed to create project")
			return
		}
	}

	projectData := projectToAPI(project)

	responseData := gin.H{
		"project": projectData,
	}

	response.Success(ctx, http.StatusCreated, responseData, "Project created successfully")
}

// GetProject handles GET /api/v1/projects/:id
func (h *ProjectHandler) GetProject(ctx *gin.Context) {
	// Get user ID from context
	userUUID, ok := utils.UserIDFromGin(ctx)
	if !ok {
		response.Fail(ctx, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	userIDStr := userUUID.String()

	projectID := ctx.Param("id")

	project, err := h.projectService.GetProjectByIDAndUserID(ctx, projectID, userIDStr)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) || errors.Is(err, service.ErrInvalidProjectID) || errors.Is(err, service.ErrInvalidUserID) {
			response.Fail(ctx, http.StatusNotFound, err, "Project not found or access denied")
			return
		}
		response.Fail(ctx, http.StatusInternalServerError, err, "Failed to retrieve project")
		return
	}

	response.Success(ctx, http.StatusOK, projectToAPI(project), "Project retrieved successfully")
}

// ListProjects handles GET /api/v1/projects
func (h *ProjectHandler) ListProjects(ctx *gin.Context) {
	userUUID, ok := utils.UserIDFromGin(ctx)
	if !ok {
		response.Fail(ctx, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	userIDStr := userUUID.String()

	projects, err := h.projectService.GetProjectsByUserID(ctx, userIDStr)
	if err != nil {
		response.Fail(ctx, http.StatusInternalServerError, err, "Failed to retrieve projects")
		return
	}

	list := make([]gin.H, 0, len(projects))
	for i := range projects {
		list = append(list, projectToAPI(&projects[i]))
	}
	response.Success(ctx, http.StatusOK, list, "Projects retrieved successfully")
}

// GetProjectAccess handles GET /api/v1/projects/:id/access
// Returns the external connection string for direct DB access via Traefik TCP SNI.
func (h *ProjectHandler) GetProjectAccess(ctx *gin.Context) {
	userUUID, ok := utils.UserIDFromGin(ctx)
	if !ok {
		response.Fail(ctx, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	userIDStr := userUUID.String()

	projectID := ctx.Param("id")

	info, err := h.projectService.GetExternalConnectionInfo(ctx, projectID, userIDStr)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrExternalAccessNotConfigured):
			response.Fail(ctx, http.StatusServiceUnavailable, err, "External database access is not configured on this server")
		case errors.Is(err, service.ErrProjectNotAccessible),
			errors.Is(err, service.ErrProjectNotFound),
			errors.Is(err, service.ErrInvalidProjectID),
			errors.Is(err, service.ErrInvalidUserID):
			response.Fail(ctx, http.StatusNotFound, err, "Project not found or access denied")
		case errors.Is(err, service.ErrNoRunningInstance):
			response.Fail(ctx, http.StatusConflict, err, "Database is not running yet")
		default:
			response.Fail(ctx, http.StatusInternalServerError, err, "Failed to retrieve connection info")
		}
		return
	}

	response.Success(ctx, http.StatusOK, gin.H{
		"connection_string": info.ConnectionString,
		"host":              info.Host,
		"port":              info.Port,
		"database":          info.Database,
		"username":          info.Username,
		"password":          info.Password,
	}, "Connection info retrieved successfully")
}

// DeleteProject handles DELETE /api/v1/projects/:id
func (h *ProjectHandler) DeleteProject(ctx *gin.Context) {
	// Get user ID from context
	userUUID, ok := utils.UserIDFromGin(ctx)
	if !ok {
		response.Fail(ctx, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	userIDStr := userUUID.String()

	projectID := ctx.Param("id")

	err := h.projectService.DeleteProjectByIDAndUserID(ctx, projectID, userIDStr)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProjectNotFound), errors.Is(err, service.ErrInvalidProjectID), errors.Is(err, service.ErrInvalidUserID):
			response.Fail(ctx, http.StatusNotFound, err, "Project not found or access denied")
			return
		default:
			response.Fail(ctx, http.StatusInternalServerError, err, "Failed to delete project")
			return
		}
	}

	response.Success(ctx, http.StatusOK, nil, "Project deleted successfully")
}
