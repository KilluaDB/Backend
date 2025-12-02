package routes

import (
	"my_project/internal/handlers"
	"my_project/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type QueryRoutes struct {
	handler *handlers.QueryHandler
}

func NewQueryRoutes(handler *handlers.QueryHandler) *QueryRoutes {
	return &QueryRoutes{handler: handler}
}

func (r *QueryRoutes) RegisterRoutes(router *gin.RouterGroup) {
	query := router.Group("/projects/:project_id/query")
	query.Use(middlewares.Authenticate)
	{
		//* Connection management endpoints
		// query.POST("/connections", r.handler.CreateConnection)
		// query.GET("/connections", r.handler.GetConnections)
		// query.GET("/connections/:id", r.handler.GetConnection)
		// query.PUT("/connections/:id", r.handler.UpdateConnection)
		// query.DELETE("/connections/:id", r.handler.DeleteConnection)
		// query.POST("/connections/test", r.handler.TestConnection)

		// Query execution endpoints
		query.POST("/execute", r.handler.ExecuteQuery)
		query.GET("/history", r.handler.GetQueryHistory)
	}
}
