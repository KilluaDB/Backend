package routes

import (
	"my_project/internal/handlers"
	"my_project/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type TextToSqlRoutes struct {
	handler *handlers.TextToSQLHandler
}

func NewTextToSqlRoutes(handler *handlers.TextToSQLHandler) *TextToSqlRoutes {
	return &TextToSqlRoutes{handler: handler}
}

func (r *TextToSqlRoutes) RegisterRoutes(router *gin.RouterGroup) {
	query := router.Group("/projects/:id/text-to-sql")
	query.Use(middlewares.Authenticate)
	{
		// Query execution endpoints
		query.POST("/generate", r.handler.GenerateSQL)
		query.POST("/execute", r.handler.GenerateAndExecuteSQL)
	}
}
