package routes

import (
	"backend/internal/handlers"
	"backend/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type QueryRoutes struct {
	handler *handlers.QueryHandler
}

func NewQueryRoutes(handler *handlers.QueryHandler) *QueryRoutes {
	return &QueryRoutes{handler: handler}
}

func (r *QueryRoutes) RegisterRoutes(router *gin.RouterGroup) {
	query := router.Group("/projects/:id/query")
	query.Use(middlewares.Authenticate)
	{
		// Query execution endpoints
		query.POST("/execute", r.handler.ExecuteQuery)
		query.GET("/history", r.handler.GetQueryHistory)
	}
}
