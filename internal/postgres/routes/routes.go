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
	projectRepo     repositories.ProjectRepository
	postgresHandler *handler.PostgresHandler
	tableHandler    *handler.TableHandler
	schemaHandler   *handler.SchemaHandler
	queryHandler    *handler.QueryHandler
}

func NewPostgresRoutes(
	projectRepo repositories.ProjectRepository,
	postgresHandler *handler.PostgresHandler,
	tableHandler *handler.TableHandler,
	schemaHandler *handler.SchemaHandler,
	queryHandler *handler.QueryHandler,
) *PostgresRoutes {
	return &PostgresRoutes{
		projectRepo:     projectRepo,
		postgresHandler: postgresHandler,
		tableHandler:    tableHandler,
		schemaHandler:   schemaHandler,
		queryHandler:    queryHandler,
	}
}

func (r *PostgresRoutes) RegisterRoutes(router *gin.RouterGroup) {
	postgres := router.Group("/projects/:id/postgres")
	postgres.Use(middlewares.Authenticate)
	postgres.Use(middlewares.RequirePostgresProject(r.projectRepo))
	{
		// Tables
		postgres.GET("/tables", r.postgresHandler.ListTables)
		postgres.POST("/tables", r.tableHandler.CreateTable)
		postgres.DELETE("/tables/:table", r.postgresHandler.DeleteTableByPath)

		// Rows
		postgres.GET("/tables/:table/rows", r.postgresHandler.GetRows)
		postgres.POST("/tables/:table/rows", r.postgresHandler.InsertRowWithTable)
		postgres.PATCH("/tables/:table/rows", r.postgresHandler.UpdateRows)
		postgres.DELETE("/tables/:table/rows/:row_id", r.postgresHandler.DeleteRows)
		postgres.DELETE("/tables/:table/rows", r.postgresHandler.DeleteRows)

		// Columns
		postgres.POST("/tables/:table/columns", r.postgresHandler.AddColumnWithTable)
		postgres.DELETE("/tables/:table/columns/:column", r.postgresHandler.DeleteColumnWithTable)

		// Schema
		postgres.GET("/schema/visualize", r.schemaHandler.VisualizeSchema)

		// Query
		postgres.POST("/query/execute", r.queryHandler.ExecuteQuery)
		postgres.GET("/query/history", r.queryHandler.GetQueryHistory)
	}
}
