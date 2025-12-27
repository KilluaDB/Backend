package routes

import (
	"my_project/internal/handlers"
	"my_project/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type SchemaRoutes struct {
	handler *handlers.SchemaHandler
}

func NewSchemaRoutes(handler *handlers.SchemaHandler) *SchemaRoutes {
	return &SchemaRoutes{handler: handler}
}

func (r *SchemaRoutes) RegisterRoutes(router *gin.RouterGroup) {
	schema := router.Group("/projects/:id/schema")
	schema.Use(middlewares.Authenticate)
	{
		schema.GET("/visualize", r.handler.VisualizeSchema)
	}
}
