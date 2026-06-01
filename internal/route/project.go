package route

import (
	"backend/internal/handler"
	"backend/internal/middleware"

	"github.com/gin-gonic/gin"
)

type ProjectRoutes struct {
	handler *handler.ProjectHandler
}

func NewProjectRoutes(handler *handler.ProjectHandler) *ProjectRoutes {
	return &ProjectRoutes{handler: handler}
}

func (r *ProjectRoutes) RegisterRoutes(router *gin.RouterGroup) {
	projects := router.Group("/projects")
	projects.Use(middleware.Authenticate) // All project routes require authentication
	{
		projects.POST("", r.handler.CreateProject)
		projects.GET("", r.handler.ListProjects)
		projects.GET("/:id", r.handler.GetProject)
		projects.GET("/:id/access", r.handler.GetProjectAccess)
		projects.DELETE("/:id", r.handler.DeleteProject)
	}
}
