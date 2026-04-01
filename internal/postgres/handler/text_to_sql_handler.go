package handler

import (
	"errors"
	"fmt"
	"net/http"

	"backend/internal/postgres/service"
	"backend/internal/responses"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TextToSQLHandler struct {
	textToSQLService *service.TextToSQLService
	queryService     *service.QueryService
}

func NewTextToSQLHandler(
	textToSQLService *service.TextToSQLService,
	queryService *service.QueryService,
) *TextToSQLHandler {
	return &TextToSQLHandler{
		textToSQLService: textToSQLService,
		queryService:     queryService,
	}
}

// GenerateSQL generates SQL from natural language without executing
// POST /api/v1/projects/:id/text-to-sql/generate
func (h *TextToSQLHandler) GenerateSQL(c *gin.Context) {
	projectID := c.Param("id")
	if projectID == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
		return
	}

	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req service.TextToSQLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if req.Question == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Question is required: Cannot be empty")
		return
	}

	// Convert userID to UUID (handle both uuid.UUID and string types)
	var userUUID uuid.UUID
	switch v := userID.(type) {
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
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	// Call FastAPI to generate SQL
	result, err := h.textToSQLService.GenerateSQL(userUUID, &req, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProjectNotFound):
			responses.Fail(c, http.StatusNotFound, err, "Project not found or not accessible")
		case errors.Is(err, service.ErrNoRunningDBInstance):
			responses.Fail(c, http.StatusBadRequest, err, "No running database instance for this project")
		case errors.Is(err, service.ErrNoDBCredentials):
			responses.Fail(c, http.StatusBadRequest, err, "Database credentials not configured")
		case errors.Is(err, service.ErrTextToSQLUnavailable):
			responses.Fail(c, http.StatusServiceUnavailable, err, fmt.Sprintf("Text-to-SQL service unavailable: %v", err))
		case errors.Is(err, service.ErrTextToSQLInvalidResponse):
			responses.Fail(c, http.StatusBadGateway, err, fmt.Sprintf("Invalid response from text-to-SQL service: %v", err))
		default:
			responses.Fail(c, http.StatusInternalServerError, err, "Text-to-SQL request failed")
		}
		return
	}

	response := gin.H{
		"result": result,
	}
	responses.Success(c, http.StatusOK, response, "Query executed successfully")
}

func (h *TextToSQLHandler) GenerateAndExecuteSQL(c *gin.Context) {
	projectID := c.Param("id")
	if projectID == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Project id is required")
		return
	}

	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	var req service.TextToSQLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}

	if req.Question == "" {
		responses.Fail(c, http.StatusBadRequest, nil, "Question is required: Cannot be empty")
		return
	}

	// Convert userID to UUID (handle both uuid.UUID and string types)
	var userUUID uuid.UUID
	switch v := userID.(type) {
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
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	// Call FastAPI to generate SQL
	genResult, err := h.textToSQLService.GenerateSQL(userUUID, &req, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProjectNotFound):
			responses.Fail(c, http.StatusNotFound, err, "Project not found or not accessible")
		case errors.Is(err, service.ErrNoRunningDBInstance):
			responses.Fail(c, http.StatusBadRequest, err, "No running database instance for this project")
		case errors.Is(err, service.ErrNoDBCredentials):
			responses.Fail(c, http.StatusBadRequest, err, "Database credentials not configured")
		case errors.Is(err, service.ErrTextToSQLUnavailable):
			responses.Fail(c, http.StatusServiceUnavailable, err, fmt.Sprintf("Text-to-SQL service unavailable: %v", err))
		case errors.Is(err, service.ErrTextToSQLInvalidResponse):
			responses.Fail(c, http.StatusBadGateway, err, fmt.Sprintf("Invalid response from text-to-SQL service: %v", err))
		default:
			responses.Fail(c, http.StatusInternalServerError, err, "Text-to-SQL request failed")
		}
		return
	}

	if !genResult.Success {
		responses.Success(c, http.StatusOK, err, "SQL generation failed")
		return
	}

	// Step 2: Execute the generated SQL using existing QueryService
	// This reuses all the validation and security measures
	execReq := &service.ExecuteQueryRequest{
		Query: genResult.SQL,
	}

	execResult, exec, err := h.queryService.ExecuteSQLQuery(userUUID, execReq, projectUUID)
	if err != nil {
		responses.Fail(c, http.StatusInternalServerError, err, "Query execution failed")
		return
	}

	response := gin.H{
		"result":            execResult,
		"execution_id":      exec.ID,
		"execution_time_ms": execResult.ExecutionTime,
	}

	responses.Success(c, http.StatusOK, response, "Query executed successfully")
}