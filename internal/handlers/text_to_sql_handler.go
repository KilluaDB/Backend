package handlers

import (
	"net/http"

	"my_project/internal/responses"
	"my_project/internal/services"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TextToSQLHandler struct {
	textToSQLService *services.TextToSQLService
	queryService     *services.QueryService
}

func NewTextToSQLHandler(
	textToSQLService *services.TextToSQLService,
	queryService *services.QueryService,
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

	var req services.TextToSQLRequest
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
		responses.Fail(c, http.StatusServiceUnavailable, err, "Text-to-SQL service unavailable")
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

	var req services.TextToSQLRequest
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
		responses.Fail(c, http.StatusServiceUnavailable, err, "Text-to-SQL service unavailable")
		return
	}

	if !genResult.Success {
		responses.Success(c, http.StatusOK, err, "SQL generation failed")
		return
	}

	// Step 2: Execute the generated SQL using existing QueryService
	// This reuses all the validation and security measures
	execReq := &services.ExecuteQueryRequest{
		Query: genResult.SQL,
	}

	execResult, exec, err := h.queryService.ExecuteQuery(userUUID, execReq, projectUUID)
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