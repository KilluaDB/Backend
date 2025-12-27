package routes

import (
	"my_project/internal/handlers"
	"my_project/internal/middlewares"

	"github.com/gin-gonic/gin"
)

type TableRoutes struct {
	tableHandler *handlers.TableHandler
}

func NewTableRoutes(tableHandler *handlers.TableHandler) *TableRoutes {
	return &TableRoutes {
		tableHandler: tableHandler,
	}
}

func (r *TableRoutes) RegisterRoutes(router *gin.RouterGroup) {
	projects := router.Group("projects/:id")
	projects.Use(middlewares.Authenticate)
	{
		// REST conventions: POST /tables (create), DELETE /tables (delete)
		projects.POST("/tables", r.tableHandler.CreateTable)
		projects.DELETE("/tables", r.tableHandler.DeleteTable)
		// Future: PUT /tables for updates, GET /tables for listing
	}
}