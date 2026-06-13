package handler

import (
	"context"
	pgservice "backend/internal/postgres/service"
	"backend/internal/response"
	"backend/internal/service"
	"backend/internal/utils"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type schemaServiceI interface {
	VisualizeSchema(userID, projectID uuid.UUID, schema string) (string, error)
	ListSchemas(ctx context.Context, userID, projectID uuid.UUID) ([]string, error)
	CachePendingDDL(ctx context.Context, projectID uuid.UUID, ddl string) error
	ApplyDDL(ctx context.Context, userID, projectID uuid.UUID) error
}

var sseClient = &http.Client{
	Timeout: 0, // streaming
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DisableCompression:    true, // do not buffer/transform SSE
		ResponseHeaderTimeout: 15 * time.Second,
	},
}

type SchemaHandler struct {
	schemaService schemaServiceI
}

func NewSchemaHandler(schemaService schemaServiceI) *SchemaHandler {
	return &SchemaHandler{
		schemaService: schemaService,
	}
}

// VisualizeSchema handles GET /api/v1/projects/:id/postgres/schema/visualize
func (h *SchemaHandler) VisualizeSchema(c *gin.Context) {
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

	schema := c.DefaultQuery("schema", "public")

	mermaid, err := h.schemaService.VisualizeSchema(c.Request.Context(), userUUID, projectUUID, schema)
	if err != nil {
		if errors.Is(err, service.ErrProjectNotFound) {
			pgFail(c, http.StatusNotFound, err, "Project not found or not accessible")
			return
		}
		pgFail(c, http.StatusInternalServerError, err, "Failed to visualize schema")
		return
	}

	response.Success(c, http.StatusOK, gin.H{
		"schema":  schema,
		"mermaid": mermaid,
	}, "Schema visualization generated successfully")
}

// ListSchemas handles GET /api/v1/projects/:id/postgres/schemas
func (h *SchemaHandler) ListSchemas(c *gin.Context) {
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

	schemas, err := h.schemaService.ListSchemas(c.Request.Context(), userUUID, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProjectNotAccessible), errors.Is(err, service.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusBadRequest, err, err.Error())
			return
		}
	}
	response.Success(c, http.StatusOK, gin.H{"schemas": schemas}, "Schemas listed successfully")
}

type generateSchemaFromTextStreamRequest struct {
	RequirementText string `json:"requirement_text" binding:"required"`
	ModelName       string `json:"model_name,omitempty"`
	DatabaseName    string `json:"database_name,omitempty"`
}

func schemaAIBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(os.Getenv("SCHEMA_AI_BASE_URL")), "/")
}

// proxySchemaSSE validates auth/project, binds the request body, and proxies
// an SSE stream from the upstream AI service at the given path suffix.
// Both the real and mock stream handlers delegate to this helper.
func (h *SchemaHandler) proxySchemaSSE(c *gin.Context, upstreamPath string) {
	_, ok := utils.UserIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	if _, err := utils.ProjectIDFromGin(c); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	var reqBody generateSchemaFromTextStreamRequest
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if strings.TrimSpace(reqBody.ModelName) == "" {
		reqBody.ModelName = "deepseek"
	}

	base := schemaAIBaseURL()
	if base == "" {
		pgFail(c, http.StatusServiceUnavailable, nil, "Schema AI service is not configured (SCHEMA_AI_BASE_URL)")
		return
	}
	upstreamURL := base + upstreamPath
	payload, err := json.Marshal(reqBody)
	if err != nil {
		pgFail(c, http.StatusInternalServerError, err, "Failed to prepare request")
		return
	}

	upReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstreamURL, bytes.NewReader(payload))
	if err != nil {
		pgFail(c, http.StatusInternalServerError, err, "Failed to create upstream request")
		return
	}
	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Accept", "text/event-stream")

	upResp, err := sseClient.Do(upReq)
	if err != nil {
		pgFail(c, http.StatusBadGateway, err, "Schema generator service is unavailable")
		return
	}
	defer upResp.Body.Close()

	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(upResp.Body, 64*1024))
		pgFail(c, http.StatusBadGateway, errors.New(string(b)), "Schema generator service returned an error")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		pgFail(c, http.StatusInternalServerError, nil, "Streaming not supported")
		return
	}

	buf := make([]byte, 32*1024)
	for {
		n, rerr := upResp.Body.Read(buf)
		if n > 0 {
			if _, werr := c.Writer.Write(buf[:n]); werr != nil {
				// client went away
				return
			}
			flusher.Flush()
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return
			}
			return
		}
	}
}

// GenerateSchemaFromTextStream handles POST /api/v1/projects/:id/postgres/schema/from-text/stream
// It proxies the real AI service SSE stream and passes events through to the client.
func (h *SchemaHandler) GenerateSchemaFromTextStream(c *gin.Context) {
	h.proxySchemaSSE(c, "/schema/generate/stream/mock")
}

// schemaGenerateResponse mirrors the AI service's SchemaGenerateResponse.
type schemaGenerateResponse struct {
	Success         bool                   `json:"success"`
	Message         string                 `json:"message"`
	Error           *string                `json:"error,omitempty"`
	Mmd             *string                `json:"mmd,omitempty"`
	MmdValid        *bool                  `json:"mmd_valid,omitempty"`
	DBSchema        map[string]interface{} `json:"db_schema,omitempty"`
	FullReport      *string                `json:"full_report,omitempty"`
	DDL             *string                `json:"ddl,omitempty"`
	IndexStatements *string                `json:"index_statements,omitempty"`
	GenerationTime  *float64               `json:"generation_time,omitempty"`
}

// GenerateSchemaFromText handles POST /api/v1/projects/:id/postgres/schema/from-text
// Non-streaming variant: proxies to the AI service's /schema/generate and returns
// the full JSON response once the multi-agent pipeline completes.
func (h *SchemaHandler) GenerateSchemaFromText(c *gin.Context) {
	_, ok := utils.UserIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	if _, err := utils.ProjectIDFromGin(c); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	var reqBody generateSchemaFromTextStreamRequest
	if err := c.ShouldBindJSON(&reqBody); err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid request body")
		return
	}
	if strings.TrimSpace(reqBody.ModelName) == "" {
		reqBody.ModelName = "deepseek"
	}

	base := schemaAIBaseURL()
	if base == "" {
		pgFail(c, http.StatusServiceUnavailable, nil, "Schema AI service is not configured (SCHEMA_AI_BASE_URL)")
		return
	}

	upstreamURL := base + "/schema/generate/mock"
	payload, err := json.Marshal(reqBody)
	if err != nil {
		pgFail(c, http.StatusInternalServerError, err, "Failed to prepare request")
		return
	}

	upReq, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, upstreamURL, bytes.NewReader(payload))
	if err != nil {
		pgFail(c, http.StatusInternalServerError, err, "Failed to create upstream request")
		return
	}
	upReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 5 * time.Minute, // LLM multi-agent pipeline can be slow
	}

	upResp, err := client.Do(upReq)
	if err != nil {
		pgFail(c, http.StatusBadGateway, err, "Schema generator service is unavailable")
		return
	}
	defer upResp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(upResp.Body, 10*1024*1024)) // 10 MB cap
	if err != nil {
		pgFail(c, http.StatusBadGateway, err, "Failed to read response from schema generator")
		return
	}

	if upResp.StatusCode < 200 || upResp.StatusCode >= 300 {
		pgFail(c, http.StatusBadGateway, errors.New(string(body)), "Schema generator service returned an error")
		return
	}

	var result schemaGenerateResponse
	if err := json.Unmarshal(body, &result); err != nil {
		pgFail(c, http.StatusBadGateway, err, "Invalid response from schema generator")
		return
	}

	if !result.Success {
		errMsg := "Schema generation failed"
		if result.Error != nil {
			errMsg = *result.Error
		}
		pgFail(c, http.StatusUnprocessableEntity, errors.New(errMsg), result.Message)
		return
	}

	projectUUID, _ := utils.ProjectIDFromGin(c)
	if result.DDL != nil {
		// Cache the pending DDL securely in Redis for the project
		_ = h.schemaService.CachePendingDDL(c.Request.Context(), projectUUID, *result.DDL)
	}

	response.Success(c, http.StatusOK, gin.H{
		"mermaid": result.Mmd,
	}, result.Message)
}

// ApplySchemaDDL handles POST /api/v1/projects/:id/postgres/schema/approve
// It executes the user-approved DDL securely cached in Redis directly on their postgres database.
// No body request is needed, which prevents raw SQL injection tampering from client payloads.
func (h *SchemaHandler) ApplySchemaDDL(c *gin.Context) {
	userID, ok := utils.UserIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectID, err := utils.ProjectIDFromGin(c)
	if err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	if err := h.schemaService.ApplyDDL(c.Request.Context(), userID, projectID); err != nil {
		if errors.Is(err, pgservice.ErrNoPendingDDL) {
			pgFail(c, http.StatusNotFound, err, "No pending schema suggestion found or it has expired")
			return
		}
		pgFail(c, http.StatusInternalServerError, err, "Failed to apply generated DDL schema")
		return
	}

	response.Success(c, http.StatusCreated, nil, "Schema suggestion applied and executed successfully")
}
