package handler

import (
	"backend/internal/postgres/service"
	"backend/internal/responses"
	"backend/internal/services"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
)

type QueryHandler struct {
	queryService *service.QueryService
}

func NewQueryHandler(queryService *service.QueryService) *QueryHandler {
	return &QueryHandler{queryService: queryService}
}

// ExecuteQuery executes a SQL query for a Postgres project.
// Body: { "query": "..." }
func (h *QueryHandler) ExecuteQuery(c *gin.Context) {
	projectId := c.Param("id")
	if projectId == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
		return
	}

	userId, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var userUUID uuid.UUID
	switch v := userId.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusUnauthorized, err, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusUnauthorized, nil, "Invalid user ID format")
		return
	}

	projectUUID, err := uuid.Parse(projectId)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	var req service.ExecuteQueryRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body: query is required")
		return
	}
	if req.Query == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Query is required: Cannot be empty")
		return
	}

	result, exec, err := h.queryService.ExecuteSQLQuery(userUUID, &req, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidQuery):
			message := "Invalid or disallowed query"
			if result != nil && result.Error != "" {
				message = result.Error
			}
			if result != nil {
				c.JSON(http.StatusBadRequest, gin.H{"status": "error", "message": message, "data": result})
			} else {
				responses.Fail(c, http.StatusBadRequest, err, message)
			}
			return
		case errors.Is(err, services.ErrProjectNotAccessible), errors.Is(err, services.ErrNoRunningInstance):
			responses.Fail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			if result != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"status": "error", "message": "Query execution failed", "data": result})
			} else {
				responses.Fail(c, http.StatusInternalServerError, err, "Query execution failed")
			}
			return
		}
	}

	execMs := int64(result.ExecutionTime)
	if exec.ExecutionTimeMs != nil {
		execMs = int64(*exec.ExecutionTimeMs)
	}

	response := gin.H{
		"result":            result,
		"execution_id":      exec.ID,
		"execution_time_ms": execMs,
	}
	responses.Success(c, http.StatusOK, response, "Query executed successfully")
}

// GetQueryHistory returns Postgres query history for the authenticated user.
func (h *QueryHandler) GetQueryHistory(c *gin.Context) {
	userId, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	limitStr := c.DefaultQuery("limit", "10")
	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 {
		limit = 10
	}
	if limit > 30 {
		limit = 30
	}

	var userUUID uuid.UUID
	switch v := userId.(type) {
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

	history, err := h.queryService.GetQueryHistory(userUUID, limit)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Failed to retrieve query history")
		return
	}

	responses.Success(c, http.StatusOK, history, "Query history retrieved successfully")
}

