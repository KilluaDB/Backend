package routes

import (
	"backend/internal/middlewares"
	"backend/internal/postgres/handler"
	"backend/internal/repositories"

	"github.com/gin-gonic/gin"
)

// PostgresRoutes registers all /projects/:id/postgres/* endpoints.
// Middleware ensures the project is PostgreSQL.
type PostgresRoutes struct {
	projectRepo repositories.ProjectRepository
	pg          *handler.PostgresHandler
}

func NewPostgresRoutes(projectRepo repositories.ProjectRepository, pg *handler.PostgresHandler) *PostgresRoutes {
	return &PostgresRoutes{
		projectRepo: projectRepo,
		pg:          pg,
	}
}

func (r *PostgresRoutes) RegisterRoutes(router *gin.RouterGroup) {
	postgres := router.Group("/projects/:id/postgres")
	postgres.Use(middlewares.Authenticate)
	postgres.Use(middlewares.RequirePostgresProject(r.projectRepo))
	h := r.pg
	{
		// Dashboard
		postgres.GET("/dashboard/overview", h.Dashboard.GetOverview)
		postgres.GET("/dashboard/metrics", h.Dashboard.GetMetrics)

		// Tables
		postgres.POST("/tables", h.Table.CreateTable)
		postgres.GET("/tables", h.Table.GetTables)
		postgres.GET("/tables/:table", h.Table.GetTable)
		postgres.PATCH("/tables/:table", h.Table.UpdateTable)
		postgres.DELETE("/tables/:table", h.Table.DeleteTable)

		// Rows
		postgres.GET("/tables/:table/rows", h.Table.GetRows)
		postgres.POST("/tables/:table/rows", h.Table.InsertRow)
		postgres.PATCH("/tables/:table/rows", h.Table.UpdateRows)
		postgres.DELETE("/tables/:table/rows", h.Table.DeleteRows)

		// Columns
		postgres.POST("/tables/:table/columns", h.Table.AddColumn)
		postgres.DELETE("/tables/:table/columns/:column", h.Table.DropColumn)

		// Indexes
		postgres.GET("/tables/:table/indexes", h.Table.ListIndexes)
		postgres.POST("/tables/:table/indexes", h.Table.CreateIndex)
		postgres.DELETE("/tables/:table/indexes/:index", h.Table.DropIndex)

		// Schema
		postgres.GET("/schemas", h.Schema.ListSchemas)
		postgres.GET("/schema/visualize", h.Schema.VisualizeSchema)
		postgres.POST("/schema/from-text/stream", h.Schema.GenerateSchemaFromTextStream)

		// Query
		postgres.POST("/query/execute", h.Query.ExecuteQuery)
		postgres.GET("/query/history", h.Query.GetQueryHistory)
	}
}
