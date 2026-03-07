package routes

import (
	"backend/internal/handlers"
	"backend/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type ContainerRoutes struct {
	handler *handlers.ContainerHandler
}

func NewContainerRoutes(handler *handlers.ContainerHandler) *ContainerRoutes {
	return &ContainerRoutes{handler: handler}
}

// RegisterRoutes registers unified, DB-agnostic routes for containers,
// records, and fields under /projects/:id.
func (r *ContainerRoutes) RegisterRoutes(router *gin.RouterGroup) {
	projects := router.Group("/projects/:id")
	projects.Use(middlewares.Authenticate)
	{
		// Containers
		projects.POST("/containers", r.handler.CreateContainer)
		projects.GET("/containers", r.handler.ListContainers)
		projects.DELETE("/containers/:container", r.handler.DeleteContainer)

		// Records
		projects.POST("/containers/:container/records", r.handler.InsertRecord)
		projects.GET("/containers/:container/records", r.handler.GetRecords)
		projects.PATCH("/containers/:container/records", r.handler.UpdateRecords)
		projects.DELETE("/containers/:container/records", r.handler.DeleteRecords)

		// Fields
		projects.POST("/containers/:container/fields", r.handler.AddField)
		projects.DELETE("/containers/:container/fields/:field", r.handler.RemoveField)
	}
}

