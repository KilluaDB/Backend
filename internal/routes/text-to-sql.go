package routes

import (
	"backend/internal/postgres/handler"
	"backend/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type TextToSqlRoutes struct {
	handler *handler.TextToSQLHandler
}

func NewTextToSqlRoutes(handler *handler.TextToSQLHandler) *TextToSqlRoutes {
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
