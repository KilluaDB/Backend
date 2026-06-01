package handler

import (
	pgservice "backend/internal/postgres/service"
	"backend/internal/response"
	"backend/internal/service"
	"backend/internal/utils"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
)

type QueryHandler struct {
	queryService *pgservice.QueryService
}

func NewQueryHandler(queryService *pgservice.QueryService) *QueryHandler {
	return &QueryHandler{queryService: queryService}
}

// ExecuteQuery executes a SQL query for a Postgres project.
func (h *QueryHandler) ExecuteQuery(c *gin.Context) {
	projectUUID, err := utils.ProjectIDFromGin(c)
	if err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req pgservice.ExecuteQueryRequest
	if err := c.ShouldBindBodyWith(&req, binding.JSON); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body: query is required")
		return
	}
	if req.Query == "" {
		pgFail(c, http.StatusBadRequest, nil, "Query is required: Cannot be empty")
		return
	}

	result, exec, err := h.queryService.ExecuteSQLQuery(c.Request.Context(), userUUID, &req, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, pgservice.ErrInvalidQuery):
			message := "Invalid or disallowed query"
			if result != nil && result.Error != "" {
				message = result.Error
			}
			if result != nil {
				pgFailWithData(c, http.StatusBadRequest, err, message, result)
			} else {
				pgFail(c, http.StatusBadRequest, err, message)
			}
			return
		case errors.Is(err, service.ErrProjectNotAccessible), errors.Is(err, service.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			message := err.Error()
			if result != nil && result.Error != "" {
				message = result.Error
			}
			if result != nil {
				pgFailWithData(c, http.StatusBadRequest, err, message, result)
			} else {
				pgFail(c, http.StatusBadRequest, err, message)
			}
			return
		}
	}

	execMs := int64(result.ExecutionTime)
	if exec.ExecutionTimeMs != nil {
		execMs = int64(*exec.ExecutionTimeMs)
	}

	res := gin.H{
		"result":            result,
		"execution_id":      exec.ID,
		"execution_time_ms": execMs,
	}
	response.Success(c, http.StatusOK, res, "Query executed successfully")
}

// GetQueryHistory returns recent pg_stat_statements data for this project's database instance.
func (h *QueryHandler) GetQueryHistory(c *gin.Context) {
	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, err := utils.ProjectIDFromGin(c)
	if err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
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

	history, err := h.queryService.GetQueryHistory(c.Request.Context(), userUUID, projectUUID, limit)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProjectNotAccessible), errors.Is(err, service.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusInternalServerError, err, "Failed to retrieve query history")
			return
		}
	}

	response.Success(c, http.StatusOK, history, "Query history retrieved successfully")
}
