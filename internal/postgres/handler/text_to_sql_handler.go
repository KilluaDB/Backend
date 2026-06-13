package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"backend/internal/postgres/service"
	"backend/internal/response"
	"backend/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type textToSQLServiceI interface {
	GenerateSQL(ctx context.Context, userID uuid.UUID, req *service.TextToSQLRequest, projectId uuid.UUID) (*service.TextToSQLResponse, error)
}

type TextToSQLHandler struct {
	textToSQLService textToSQLServiceI
	queryService     *service.QueryService
}

func NewTextToSQLHandler(
	textToSQLService textToSQLServiceI,
	queryService *service.QueryService,
) *TextToSQLHandler {
	return &TextToSQLHandler{
		textToSQLService: textToSQLService,
		queryService:     queryService,
	}
}

func (h *TextToSQLHandler) GenerateAndExecuteSQL(c *gin.Context) {
	projectID := c.Param("id")
	if projectID == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Project id is required")
		return
	}

	userUUID, ok := utils.UserIDFromGin(c)
	if !ok {
		response.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req service.TextToSQLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if req.Question == "" {
		response.Fail(c, http.StatusBadRequest, nil, "Question is required: Cannot be empty")
		return
	}

	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		response.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	// Call FastAPI to generate SQL
	genResult, err := h.textToSQLService.GenerateSQL(c.Request.Context(), userUUID, &req, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProjectNotFound):
			response.Fail(c, http.StatusNotFound, err, "Project not found or not accessible")
		case errors.Is(err, service.ErrNoRunningDBInstance):
			response.Fail(c, http.StatusBadRequest, err, "No running database instance for this project")
		case errors.Is(err, service.ErrNoDBCredentials):
			response.Fail(c, http.StatusBadRequest, err, "Database credentials not configured")
		case errors.Is(err, service.ErrTextToSQLUnavailable):
			response.Fail(c, http.StatusServiceUnavailable, err, fmt.Sprintf("Text-to-SQL service unavailable: %v", err))
		case errors.Is(err, service.ErrTextToSQLInvalidResponse):
			response.Fail(c, http.StatusBadGateway, err, fmt.Sprintf("Invalid response from text-to-SQL service: %v", err))
		default:
			response.Fail(c, http.StatusInternalServerError, err, "Text-to-SQL request failed")
		}
		return
	}

	if !genResult.Success {
		response.Success(c, http.StatusOK, err, "SQL generation failed")
		return
	}

	// Step 2: Execute the generated SQL using existing QueryService
	// This reuses all the validation and security measures
	execReq := &service.ExecuteQueryRequest{
		Query: genResult.SQL,
	}

	execResult, exec, err := h.queryService.ExecuteSQLQuery(c.Request.Context(), userUUID, execReq, projectUUID)
	if err != nil {
		response.Fail(c, http.StatusInternalServerError, err, "Query execution failed")
		return
	}

	res := gin.H{
		"sql":               genResult.SQL,
		"result":            execResult,
		"execution_id":      exec.ID,
		"execution_time_ms": execResult.ExecutionTime,
	}

	response.Success(c, http.StatusOK, res, "Query executed successfully")
}
