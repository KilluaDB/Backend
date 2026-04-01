package handler

import (
	"backend/internal/postgres/model"
	"backend/internal/postgres/service"
	"backend/internal/responses"
	"backend/internal/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// PostgresHandler handles /projects/:id/postgres/* endpoints (tables list, delete table).
// Row/column endpoints are handled by TableHandler.
type PostgresHandler struct {
	projectService *services.ProjectService
	tableService   *service.TableService
	recordService  *services.RecordService
}

func NewPostgresHandler(
	projectService *services.ProjectService,
	tableService *service.TableService,
	recordService *services.RecordService,
) *PostgresHandler {
	return &PostgresHandler{
		projectService: projectService,
		tableService:   tableService,
		recordService:  recordService,
	}
}

func postgresGetUserUUID(c *gin.Context) (uuid.UUID, bool) {
	userID, exists := c.Get("userId")
	if !exists {
		return uuid.Nil, false
	}
	switch v := userID.(type) {
	case uuid.UUID:
		return v, true
	case string:
		u, err := uuid.Parse(v)
		if err != nil {
			return uuid.Nil, false
		}
		return u, true
	default:
		return uuid.Nil, false
	}
}

func postgresGetProjectUUID(c *gin.Context) (uuid.UUID, bool) {
	idStr := c.Param("id")
	if idStr == "" {
		return uuid.Nil, false
	}
	u, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, false
	}
	return u, true
}

// ListTables GET /postgres/tables
func (h *PostgresHandler) ListTables(c *gin.Context) {
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	tables, err := h.recordService.ListContainers(c.Request.Context(), projectUUID, userUUID)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to list tables")
		return
	}
	responses.Success(c, http.StatusOK, tables, "Tables retrieved successfully")
}

// DeleteTableByPath DELETE /postgres/tables/:table
func (h *PostgresHandler) DeleteTableByPath(c *gin.Context) {
	table := c.Param("table")
	if table == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Table name is required")
		return
	}
	userUUID, ok := postgresGetUserUUID(c)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, ok := postgresGetProjectUUID(c)
	if !ok {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid project ID")
		return
	}
	req := model.DeleteTableRequest{Schema: "public", Table: table}
	result, err := h.tableService.DeleteTable(&req, userUUID, projectUUID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Failed to delete table")
		return
	}
	responses.Success(c, http.StatusOK, gin.H{"result": result}, "Table deleted successfully")
}

