package handlers

import (
	"fmt"
	"my_project/internal/responses"
	"my_project/internal/services"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type SchemaHandler struct {
	schemaService *services.SchemaService
}

func NewSchemaHandler(schemaService *services.SchemaService) *SchemaHandler {
	return &SchemaHandler{
		schemaService: schemaService,
	}
}

// VisualizeSchema handles GET /api/v1/projects/:id/schema/visualize
func (h *SchemaHandler) VisualizeSchema(c *gin.Context) {
	// Get user ID from context (set by auth middleware)
	userID, exists := c.Get("userId")
	if !exists {
		responses.Fail(c, http.StatusUnauthorized, nil, "Unauthorized")
		return
	}

	projectID := c.Param("id")
	schema := c.DefaultQuery("schema", "public") // Default to "public" schema

	// Convert userID to uuid.UUID
	var userUUID uuid.UUID
	switch v := userID.(type) {
	case uuid.UUID:
		userUUID = v
	case string:
		parsed, err := uuid.Parse(v)
		if err != nil {
			responses.Fail(c, http.StatusBadRequest, err, "Invalid user ID format")
			return
		}
		userUUID = parsed
	default:
		responses.Fail(c, http.StatusBadRequest, nil, "Invalid user ID type")
		return
	}

	// Parse project ID
	projectUUID, err := uuid.Parse(projectID)
	if err != nil {
		responses.Fail(c, http.StatusBadRequest, err, "Invalid project ID format")
		return
	}

	// Generate visualization
	mermaidDiagram, err := h.schemaService.VisualizeSchema(userUUID, projectUUID, schema)
	if err != nil {
		fmt.Printf("ERROR in VisualizeSchema handler: %v\n", err)
		responses.Fail(c, http.StatusInternalServerError, err, fmt.Sprintf("Failed to visualize schema: %v", err))
		return
	}

	responses.Success(c, http.StatusOK, gin.H{
		"mermaid": mermaidDiagram,
		"schema":  schema,
	}, "Schema visualization generated successfully")
}
