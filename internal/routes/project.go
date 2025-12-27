package routes

import (
	"backend/internal/handlers"
	"backend/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type ProjectRoutes struct {
	handler *handlers.ProjectHandler
}

func NewProjectRoutes(handler *handlers.ProjectHandler) *ProjectRoutes {
	return &ProjectRoutes{handler: handler}
}

func (r *ProjectRoutes) RegisterRoutes(router *gin.RouterGroup) {
	projects := router.Group("/projects")
	projects.Use(middlewares.Authenticate) // All project routes require authentication
	{
		projects.POST("", r.handler.CreateProject)
		projects.GET("", r.handler.ListProjects)
		projects.GET("/:id", r.handler.GetProject)
		projects.DELETE("/:id", r.handler.DeleteProject)

		// Insert / Delete ROW(S)
		projects.POST("/:id/rows", r.handler.InsertRow)
		projects.DELETE("/:id/rows/:row_id", r.handler.DeleteRow)

		// Insert / Delete COLUMN(S)
		projects.POST("/:id/columns", r.handler.AddColumn)
		projects.DELETE("/:id/columns/:column_name", r.handler.DeleteColumn)
	}
}
