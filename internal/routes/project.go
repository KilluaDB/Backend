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
	}
}
