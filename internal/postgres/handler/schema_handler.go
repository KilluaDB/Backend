package handler

import (
	"backend/internal/postgres/service"
	"backend/internal/responses"
	"backend/internal/services"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

type SchemaHandler struct {
	schemaService *service.SchemaService
}

func NewSchemaHandler(schemaService *service.SchemaService) *SchemaHandler {
	return &SchemaHandler{
		schemaService: schemaService,
	}
}

// VisualizeSchema handles GET /api/v1/projects/:id/postgres/schema/visualize
func (h *SchemaHandler) VisualizeSchema(c *gin.Context) {
	userUUID, ok := userIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectUUID, err := projectIDFromGin(c)
	if err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	rawSchema := c.Query("schema")
	if err := service.ValidatePostgresSchemaName(rawSchema); err != nil {
		pgFail(c, http.StatusBadRequest, err, err.Error())
		return
	}
	schema := service.PostgresSchema(rawSchema)

	mermaidDiagram, err := h.schemaService.VisualizeSchema(userUUID, projectUUID, schema)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidSchema):
			pgFail(c, http.StatusBadRequest, err, "Invalid schema name")
			return
		case errors.Is(err, services.ErrProjectNotAccessible), errors.Is(err, services.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusInternalServerError, err, "Failed to visualize schema")
			return
		}
	}

	responses.Success(c, http.StatusOK, gin.H{
		"mermaid": mermaidDiagram,
		"schema":  schema,
	}, "Schema visualization generated successfully")
}

// ListSchemas handles GET /api/v1/projects/:id/postgres/schemas
func (h *SchemaHandler) ListSchemas(c *gin.Context) {
	userUUID, ok := userIDFromGin(c)
	if !ok {
		pgFail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}
	projectUUID, err := projectIDFromGin(c)
	if err != nil {
		pgFail(c, http.StatusBadRequest, err, "Invalid projectId format")
		return
	}

	schemas, err := h.schemaService.ListSchemas(c.Request.Context(), userUUID, projectUUID)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrProjectNotAccessible), errors.Is(err, services.ErrNoRunningInstance):
			pgFail(c, http.StatusNotFound, err, "Project not found or database instance not ready")
			return
		default:
			pgFail(c, http.StatusBadRequest, err, err.Error())
			return
		}
	}
	responses.Success(c, http.StatusOK, gin.H{"schemas": schemas}, "Schemas listed successfully")
}
