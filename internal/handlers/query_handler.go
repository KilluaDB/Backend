package handlers

import (
	"my_project/internal/responses"
	"my_project/internal/services"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type QueryHandler struct {
	queryService *services.QueryService
}

func NewQueryHandler(queryService *services.QueryService) *QueryHandler {
	return &QueryHandler{
		queryService: queryService,
	}
}

// ExecuteQuery executes a SQL query on the specified database connection
func (h *QueryHandler) ExecuteQuery(c *gin.Context) {
	projectId := c.Param("project_id")
	if projectId == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
		return
	}

	userId, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req services.ExecuteQueryRequest 
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body: query is required")
		return
	}

	if req.Query == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Query is required: Cannot be empty")
		return
	}

	// userId is set as a string in the auth middleware; parse to UUID
	userIdStr, ok := userId.(string)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	userUUID, err := uuid.Parse(userIdStr)
	if err != nil {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectUUID, err := uuid.Parse(projectId)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid projectId format")
		return
	}

	result, exec, err := h.queryService.ExecuteQuery(userUUID, &req, projectUUID)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to execute query")
		return
	}

	response := gin.H{
		"result":            result,
		"execution_id":      exec.ID,
		"execution_time_ms": result.ExecutionTime,
	}

	responses.Success(c, http.StatusOK, response, "Query executed successfully")
}

// GetQueryHistory returns query execution history for the authenticated user
func (h *QueryHandler) GetQueryHistory(c *gin.Context) {
	userId, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	// query param for the limit
	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 10	// min
	}
	if limit > 30 {
		limit = 30 	// max
	}

	userUUID, ok := userId.(uuid.UUID)
	if !ok {
		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
		return
	}

	history, err := h.queryService.GetQueryHistory(userUUID, limit)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to get query history")
		return
	}

	responses.Success(c, http.StatusOK, history, "Query history retrieved successfully")
}