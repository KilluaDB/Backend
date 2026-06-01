package handler

import (
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
)

type SchemaHandler struct {
	schemaService *pgservice.SchemaService
}

func NewSchemaHandler(schemaService *pgservice.SchemaService) *SchemaHandler {
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

	rawSchema := c.Query("schema")
	if err := pgservice.ValidatePostgresSchemaName(rawSchema); err != nil {
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	schema := pgservice.PostgresSchema(rawSchema)

	mermaidDiagram, err := h.schemaService.VisualizeSchema(userUUID, projectUUID, schema)
	if err != nil {
		switch {
		case errors.Is(err, pgservice.ErrInvalidSchema):
			pgFail(c, http.StatusBadRequest, err, "Invalid schema name")
			return
		case errors.Is(err, service.ErrProjectNotAccessible), errors.Is(err, service.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusInternalServerError, err, "Failed to visualize schema")
			return
		}
	}

	response.Success(c, http.StatusOK, gin.H{
		"mermaid": mermaidDiagram,
		"schema":  schema,
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
	base := strings.TrimSpace(os.Getenv("SCHEMA_AI_BASE_URL"))
	if base == "" {
		return "http://localhost:8090"
	}
	return strings.TrimRight(base, "/")
}

// GenerateSchemaFromTextStream handles POST /api/v1/projects/:id/postgres/schema/from-text/stream
// It proxies the local AI service SSE stream and passes events through to the client.
func (h *SchemaHandler) GenerateSchemaFromTextStream(c *gin.Context) {
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

	upstreamURL := schemaAIBaseURL() + "/schema/generate/stream/mock"
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

	client := &http.Client{
		Timeout: 0, // streaming
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DisableCompression:    true, // do not buffer/transform SSE
			ResponseHeaderTimeout: 15 * time.Second,
		},
	}

	upResp, err := client.Do(upReq)
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
