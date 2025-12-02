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

	userUUID, ok := userId.(uuid.UUID)
	if !ok {
		responses.Fail(c, http.StatusInternalServerError, nil, "Invalid userId format")
		return
	}

	projectUUID, err := uuid.Parse(projectId)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, nil, "Invalid projectId format")
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
